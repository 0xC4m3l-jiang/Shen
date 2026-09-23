package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"time"
)

// 本文件是**场景包的装载与校验**（方案 `C04`：数据伪装 —— 同一场景包的标识 / 数量 / 分页关系必须一致）。
//
// 设计要点（三条，都是"宁可拒绝也不带坏数据跑"）：
//
//	① **场景是数据**（`ST-24` 精神）：包写在 JSON 文件里，由运维/业务方交付；代码里只留一份内置默认包；
//	② **内置包也走同一套校验**：默认包不是特例 —— 它由同一份 `packDoc` 构造并**过同一个 `validatePack`**，
//	   这样"默认场景合法"与"外部场景合法"用的是同一条判据（默认包不可能悄悄漂移出规则之外）；
//	③ **拒绝即显式失败**：未知键、保留域之外的主机名、真实密钥、指向真实基础设施的串、审计引用不存在的账号
//	   —— 一律报错并指到具体条目，不静默丢弃、不带着坏场景起服务。
//
// 契约正文见 [`../../docs/spec/decoy-scenario.md`](../../docs/spec/decoy-scenario.md)。

// PackSchemaVersion 是本端**能读懂**的场景包版本。
const PackSchemaVersion = 1

// maxPackBytes 是场景包文件大小上限（配额：它是素材，不是数据仓库）。
const maxPackBytes = 1 << 20 // 1 MiB

// reservedTLDs 是**保留域**后缀：合成主机名必须落在其中（永不解析到真实主机）。
//
// 依据 RFC 2606 / RFC 6761：`.example` / `.test` / `.invalid` 保留给文档与测试，
// `.localhost` 保留给本机。它们**不可能**指向真实基础设施 —— 这正是我们要的保证。
var reservedTLDs = []string{".example", ".test", ".invalid", ".localhost"}

// sensitiveKeyRe 是"看起来像凭证"的配置键（其值必须是占位符）。
var sensitiveKeyRe = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|credential|private[_-]?key)`)

// leakRe 是**指向真实基础设施**的模式（私网地址、内网后缀、本机）。
var leakRe = regexp.MustCompile(`(?i)(\b10\.\d{1,3}\.\d{1,3}\.\d{1,3}\b|\b172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}\b|\b192\.168\.\d{1,3}\.\d{1,3}\b|127\.0\.0\.1|\.internal\b|\.local\b|\.lan\b|\.corp\b)`)

// packDoc 是场景包的线格式（字段名即契约，见 spec/decoy-scenario.md §1）。
type packDoc struct {
	PackVersion int           `json:"pack_version"`
	Scenarios   []scenarioDoc `json:"scenarios"`
}

type scenarioDoc struct {
	ID          string      `json:"id"`
	Org         string      `json:"org"`
	Product     string      `json:"product"`
	Host        string      `json:"host"`
	Outcome     string      `json:"outcome"` // demo | fail
	PageSize    int         `json:"page_size"`
	MaxPageSize int         `json:"max_page_size"`
	Users       []userDoc   `json:"users"`
	Config      []configDoc `json:"config"`
	Audit       []auditDoc  `json:"audit"`
}

type userDoc struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Enabled  *bool  `json:"enabled"`
	LastSeen string `json:"last_seen"`
}

type configDoc struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type auditDoc struct {
	At     string `json:"at"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Target string `json:"target"`
}

// LoadPacks 读取并校验场景包文件，返回 "场景 id → 场景"。
//
// 任何不合规都**返回错误**（不静默丢弃条目）—— 场景是素材，坏素材会让"看起来像真的"变成"一眼假"。
func LoadPacks(path string) (map[string]Scenario, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("web: 场景包路径为空")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("web: 打开场景包失败：%w", err)
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(io.LimitReader(f, maxPackBytes+1))
	if err != nil {
		return nil, fmt.Errorf("web: 读场景包失败：%w", err)
	}
	if len(raw) > maxPackBytes {
		return nil, fmt.Errorf("web: 场景包超过 %d 字节上限", maxPackBytes)
	}

	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields() // 未知键一律拒绝：静默忽略错键等于素材悄悄失效
	var doc packDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("web: 场景包不是合法 JSON（或用到了未登记的键）：%w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("web: 场景包只允许包含一个 JSON 文档")
	}

	return buildPacks(doc)
}

// buildPacks 校验整份文档并构造场景（内置包也走这里）。
func buildPacks(doc packDoc) (map[string]Scenario, error) {
	if doc.PackVersion != PackSchemaVersion {
		return nil, fmt.Errorf("web: 场景包 pack_version=%d，本端只支持 %d", doc.PackVersion, PackSchemaVersion)
	}
	if len(doc.Scenarios) == 0 {
		return nil, errors.New("web: 场景包里没有任何场景（scenarios 为空）")
	}
	out := make(map[string]Scenario, len(doc.Scenarios))
	for i, sc := range doc.Scenarios {
		where := fmt.Sprintf("scenarios[%d]", i)
		if err := validateScenarioDoc(sc, where); err != nil {
			return nil, err
		}
		if _, dup := out[sc.ID]; dup {
			return nil, fmt.Errorf("web: %s.id=%q 重复 —— 场景 id 必须唯一", where, sc.ID)
		}
		out[sc.ID] = scenarioFromDoc(sc)
	}
	return out, nil
}

// validateScenarioDoc 是场景包的**唯一判据**（内置包与外部包共用）。
func validateScenarioDoc(sc scenarioDoc, where string) error {
	if strings.TrimSpace(sc.ID) == "" {
		return fmt.Errorf("web: %s.id 不能为空", where)
	}
	if strings.TrimSpace(sc.Org) == "" {
		return fmt.Errorf("web: %s.org 不能为空", where)
	}
	if strings.TrimSpace(sc.Product) == "" {
		return fmt.Errorf("web: %s.product 不能为空", where)
	}
	if !hostIsReserved(sc.Host) {
		return fmt.Errorf("web: %s.host=%q 必须落在保留域（%s）—— 合成主机名不得指向真实基础设施",
			where, sc.Host, strings.Join(reservedTLDs, " / "))
	}
	if sc.Outcome != string(LoginDemo) && sc.Outcome != string(LoginFails) {
		return fmt.Errorf("web: %s.outcome=%q 未登记（允许：demo / fail）", where, sc.Outcome)
	}
	if sc.PageSize <= 0 {
		return fmt.Errorf("web: %s.page_size 必须为正", where)
	}
	if sc.MaxPageSize < sc.PageSize {
		return fmt.Errorf("web: %s.max_page_size(%d) 不得小于 page_size(%d)", where, sc.MaxPageSize, sc.PageSize)
	}
	if len(sc.Users) == 0 {
		return fmt.Errorf("web: %s.users 不能为空（场景至少要有账号）", where)
	}

	known := map[string]bool{}
	for j, u := range sc.Users {
		p := fmt.Sprintf("%s.users[%d]", where, j)
		if strings.TrimSpace(u.ID) == "" || strings.TrimSpace(u.Name) == "" || strings.TrimSpace(u.Role) == "" {
			return fmt.Errorf("web: %s 的 id / name / role 都不能为空", p)
		}
		if known[u.ID] {
			return fmt.Errorf("web: %s.id=%q 与前面的账号重复", p, u.ID)
		}
		known[u.ID] = true
		if u.Enabled == nil {
			return fmt.Errorf("web: %s.enabled 不能为空（显式写 true / false）", p)
		}
		if err := checkSyntheticText(u.ID+" "+u.Name+" "+u.Role, p); err != nil {
			return err
		}
		if _, err := time.Parse(time.RFC3339, u.LastSeen); err != nil {
			return fmt.Errorf("web: %s.last_seen=%q 不是 RFC3339 时间戳", p, u.LastSeen)
		}
	}

	seenKey := map[string]bool{}
	for j, c := range sc.Config {
		p := fmt.Sprintf("%s.config[%d]", where, j)
		if strings.TrimSpace(c.Key) == "" {
			return fmt.Errorf("web: %s.key 不能为空", p)
		}
		if seenKey[c.Key] {
			return fmt.Errorf("web: %s.key=%q 重复", p, c.Key)
		}
		seenKey[c.Key] = true
		if sensitiveKeyRe.MatchString(c.Key) && !isPlaceholder(c.Value) {
			return fmt.Errorf("web: %s.key=%q 是凭证类配置，值必须是占位符（含 PLACEHOLDER / REDACTED）—— "+
				"场景包禁止携带可用凭证", p, c.Key)
		}
		if err := checkSyntheticText(c.Key+" "+c.Value, p); err != nil {
			return err
		}
	}

	for j, e := range sc.Audit {
		p := fmt.Sprintf("%s.audit[%d]", where, j)
		if _, err := time.Parse(time.RFC3339, e.At); err != nil {
			return fmt.Errorf("web: %s.at=%q 不是 RFC3339 时间戳", p, e.At)
		}
		if !known[e.Actor] {
			return fmt.Errorf("web: %s.actor=%q 不在 users 里 —— 场景不自洽（审计只能引用场景内账号）", p, e.Actor)
		}
		if strings.TrimSpace(e.Action) == "" || strings.TrimSpace(e.Target) == "" {
			return fmt.Errorf("web: %s.action / target 不能为空", p)
		}
		// target 既可以是场景内账号，也可以是场景内资源名；引用账号时必须存在。
		if strings.HasPrefix(e.Target, "u-") && !known[e.Target] {
			return fmt.Errorf("web: %s.target=%q 不在 users 里", p, e.Target)
		}
		if err := checkSyntheticText(e.Action+" "+e.Target, p); err != nil {
			return err
		}
	}
	return nil
}

// hostIsReserved 判断主机名是否落在保留域（`.example` / `.test` / `.invalid` / `.localhost`）。
func hostIsReserved(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return false
	}
	// 字面 IP 一律不算合成主机名（哪怕它在保留段里）：合成场景只应用域名。
	if _, err := netip.ParseAddr(h); err == nil {
		return false
	}
	for _, tld := range reservedTLDs {
		if strings.HasSuffix(h, tld) {
			return true
		}
	}
	return false
}

// checkSyntheticText 拒绝指向真实基础设施的串（私网地址、内网后缀、本机）。
func checkSyntheticText(text, where string) error {
	if m := leakRe.FindString(text); m != "" {
		return fmt.Errorf("web: %s 里出现了 %q —— 场景包禁止指向真实基础设施（私网地址 / 内网后缀 / 本机）", where, m)
	}
	return nil
}

// isPlaceholder 判断一个值是否是**占位符**（而不是可用凭证）。
func isPlaceholder(v string) bool {
	up := strings.ToUpper(v)
	return strings.Contains(up, "PLACEHOLDER") || strings.Contains(up, "REDACTED")
}

// scenarioFromDoc 把（已校验的）文档转成运行期场景。
func scenarioFromDoc(sc scenarioDoc) Scenario {
	s := Scenario{
		ID:          sc.ID,
		Org:         sc.Org,
		Product:     sc.Product,
		Host:        sc.Host,
		Outcome:     LoginOutcome(sc.Outcome),
		PageSize:    sc.PageSize,
		MaxPageSize: sc.MaxPageSize,
	}
	for _, u := range sc.Users {
		s.Users = append(s.Users, User{
			ID: u.ID, Name: u.Name, Role: u.Role, Enabled: *u.Enabled, LastSeen: u.LastSeen,
		})
	}
	for _, c := range sc.Config {
		// 字段集与 ConfigItem 完全一致 ⇒ 直接用转换（staticcheck S1016）。
		s.Config = append(s.Config, ConfigItem(c))
	}
	for _, e := range sc.Audit {
		s.Audit = append(s.Audit, AuditEvent(e))
	}
	return s
}
