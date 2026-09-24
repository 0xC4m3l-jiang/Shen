package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"shen/common/core/internal/contract"
	"shen/common/core/internal/store"
)

// 配置文档的解析结构。
//
// 必填项一律用指针：nil 表示「键不存在」，与「值为零值」区分开。
// 缺键必须被拒绝，禁止静默补默认值 —— 否则配置写错了进程还照跑。
// 字段与约束见 docs/spec/config.md（改一处必须改另一处）。
type configDoc struct {
	Core       *coreDoc       `yaml:"core"`
	Shadow     *bool          `yaml:"shadow"`
	Session    *sessionDoc    `yaml:"session"`
	Thresholds *thresholdsDoc `yaml:"thresholds"`
	Guard      *guardDoc      `yaml:"guard"`
	Store      *storeDoc      `yaml:"store"`
	Policy     *policyDoc     `yaml:"policy"`
	Rules      *[]ruleDoc     `yaml:"rules"`
	Whitelist  *whitelistDoc  `yaml:"whitelist"`
	Decoys     *decoysDoc     `yaml:"decoys"`
	Honeypots  *[]honeypotDoc `yaml:"honeypots"`
	Injects    *[]injectDoc   `yaml:"injects"`
	AI         *aiDoc         `yaml:"ai"`
}

// injectDoc 是一条**响应改写规则**（数据，不是代码分支 —— ST-24）。
//
// 注意与 `decoys` 的区别：诱饵资产定义「投放什么钩子」，本段定义「往改道侧响应里插什么」。
type injectDoc struct {
	Kind    *string `yaml:"kind"`    // 可选；登记值见 contract.InjectKinds
	Snippet *string `yaml:"snippet"` // 必填
	Marker  *string `yaml:"marker"`  // 可选；空 = </body>
}

type coreDoc struct {
	Listen *string `yaml:"listen"`
}

type sessionDoc struct {
	CookieName *string `yaml:"cookie_name"`
}

type thresholdsDoc struct {
	RouteMirage *float64 `yaml:"route_mirage"`
	Block       *float64 `yaml:"block"`
}

type guardDoc struct {
	FalseRouteBudget *float64 `yaml:"false_route_budget"`
}

type storeDoc struct {
	Driver     *string      `yaml:"driver"`
	Redis      *redisDoc    `yaml:"redis"`
	ClickHouse *chDoc       `yaml:"clickhouse"`
	Postgres   *postgresDoc `yaml:"postgres"`
}

type redisDoc struct {
	Addr     *string `yaml:"addr"`
	Password *string `yaml:"password"`
}

type chDoc struct {
	Addr     *string `yaml:"addr"`
	Database *string `yaml:"database"`
}

type postgresDoc struct {
	DSN *string `yaml:"dsn"`
}

type policyDoc struct {
	PolicyID *string `yaml:"policy_id"`
	Version  *int64  `yaml:"version"`
	GrayPct  *int64  `yaml:"gray_pct"`
}

type ruleDoc struct {
	ID     *string   `yaml:"id"`
	Weight *float64  `yaml:"weight"`
	Match  *matchDoc `yaml:"match"`
}

type matchDoc struct {
	Field *string `yaml:"field"`
	Op    *string `yaml:"op"`
	Value *string `yaml:"value"`
}

type whitelistDoc struct {
	SourceCIDRs  *[]string `yaml:"source_cidrs"`
	UserAgents   *[]string `yaml:"user_agents"`
	PathPrefixes *[]string `yaml:"path_prefixes"`
}

// decoysDoc 是诱饵面配置：**资产清单**（数据，不是代码分支 —— ST-24 / ADR-0010）。
type decoysDoc struct {
	Assets *[]decoyAssetDoc `yaml:"assets"`
}

type decoyAssetDoc struct {
	ID   *string `yaml:"id"`
	Kind *string `yaml:"kind"`
	Path *string `yaml:"path"`
	// Hosts 是归属声明（`W7`）：启用中的资产必须至少声明一条主机名。
	Hosts   *[]string `yaml:"hosts"`
	Content *string   `yaml:"content"`
	Backend *string   `yaml:"backend"`
	Enabled *bool     `yaml:"enabled"`
}

// honeypotDoc 是幻境后端池的一项（逻辑名 → 具体蜜罐实例）。
//
// 具体蜜罐**不由本项目实现**（ADR-0011）—— 这里只登记与开关。
type honeypotDoc struct {
	Name    *string `yaml:"name"`
	Type    *string `yaml:"type"`
	Addr    *string `yaml:"addr"`
	Enabled *bool   `yaml:"enabled"`
}

// Loader 装载结果：持有构造后不可变的策略快照。
//
// AR-9（核心必须无状态多副本、进程内禁止模块级可变容器）：本类型没有模块级变量，
// 快照在 Load 时一次构造、之后只读；它不承载请求级状态。
type Loader struct {
	snap   contract.PolicySnapshot
	thr    contract.Thresholds
	shadow bool
	// ai 是 `ai:` 段的运行时配置（未配置即默认全关，见 docs/spec/config.md §2.13）。
	ai        contract.AIConfig
	decoys    []contract.DecoyAsset
	honeypots []contract.HoneypotBackend
	wl        contract.Whitelist
	// injects 为 nil 表示**未配置**该段（与「配置为空数组」语义不同，见 Injects）。
	injects *[]contract.InjectRule
	// sessionCookie 是业务自身的 session cookie 名（`session.cookie_name`）。
	//
	// 它此前**只被校验、没被消费**（cmd/core 里写死 `"sid"`）—— 客户把名字改成 `PHPSESSID` 时，
	// 核心仍在找 `sid`，会话身份会静默退化成 TLS 指纹 / IP+UA（口径与适配器也对不上）。
	sessionCookie string

	// ── 「写了、但本期不消费」的项（只用于启动日志点名，不参与任何决策）──────────────
	// 它们都是**允许留空**的可选段；记下来是为了让"配置看起来生效其实没生效"这件事在运行时可发现。
	coreListen  string  // core.listen（实际监听地址取 SHEN_LISTEN）
	guardBudget float64 // guard.false_route_budget（误调度率护栏属阶段 2b+）
	storeExtra  bool    // store.{redis,clickhouse,postgres}（driver 只支持 memory）
}

// Load 解析并校验配置，产出不可变快照。
//
// 纯计算 + 一次内存分配：不读文件、不写存储、不起网络，因此单测不需要任何替身（MD-22）。
// 任何非法输入都必须返回错误 —— 调用方据此让进程启动失败，而不是带着坏配置运行。
func Load(r io.Reader) (*Loader, error) {
	if r == nil {
		return nil, errors.New("policy: 配置读取器不能为 nil")
	}

	dec := yaml.NewDecoder(r)
	dec.KnownFields(true) // 未知键一律拒绝：静默忽略错键等于配置失效

	var d configDoc
	switch err := dec.Decode(&d); {
	case errors.Is(err, io.EOF):
		return nil, errors.New("policy: 配置内容为空")
	case err != nil:
		return nil, fmt.Errorf("policy: 解析配置失败（未知键或类型不匹配）：%w", err)
	}

	// 只允许一个文档：多文档会让「哪一份生效」产生歧义。
	var extra configDoc
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("policy: 解析配置失败：%w", err)
		}
		return nil, errors.New("policy: 配置只允许包含一个 YAML 文档")
	}

	rules, err := d.validate()
	if err != nil {
		return nil, err
	}
	return d.build(rules)
}

// Rules 返回规则集的副本。
//
// 返回副本而非内部切片：调用方改写返回值必须不影响后续判定（快照不可变）。
func (l *Loader) Rules(context.Context) ([]contract.Rule, error) {
	out := make([]contract.Rule, len(l.snap.Rules))
	copy(out, l.snap.Rules)
	return out, nil
}

// Snapshot 返回策略快照的副本。
func (l *Loader) Snapshot(context.Context) (contract.PolicySnapshot, error) {
	s := l.snap
	s.Payload = append([]byte(nil), l.snap.Payload...)
	s.Rules = make([]contract.Rule, len(l.snap.Rules))
	copy(s.Rules, l.snap.Rules)
	return s, nil
}

// Thresholds 返回决策阈值。消费方是 director（ST-23 / INT-24）。
func (l *Loader) Thresholds(context.Context) (contract.Thresholds, error) {
	return l.thr, nil
}

// GrayPct 返回灰度比例（0..100）。消费方是 director（INT-12）。
func (l *Loader) GrayPct(context.Context) (uint8, error) {
	return l.snap.GrayPct, nil
}

// Whitelist 返回白名单（INT-25）。消费方是 director 的引流判定前置：
// 内部 IP / 健康检查 / 监控探针**必须**在引流判定前生效。
func (l *Loader) Whitelist(context.Context) (contract.Whitelist, error) {
	w := l.wl
	w.SourceCIDRs = append([]netip.Prefix(nil), l.wl.SourceCIDRs...)
	w.UserAgents = append([]string(nil), l.wl.UserAgents...)
	w.PathPrefixes = append([]string(nil), l.wl.PathPrefixes...)
	return w, nil
}

// buildWhitelist 把配置转成白名单（validateWhitelist 已保证 CIDR 可解析）。
func (d *configDoc) buildWhitelist() contract.Whitelist {
	w := contract.Whitelist{
		UserAgents:   nonNil(*d.Whitelist.UserAgents),
		PathPrefixes: nonNil(*d.Whitelist.PathPrefixes),
	}
	for _, c := range *d.Whitelist.SourceCIDRs {
		if p, err := netip.ParsePrefix(c); err == nil {
			w.SourceCIDRs = append(w.SourceCIDRs, p)
		}
	}
	return w
}

// Shadow 报告是否处于影子模式。消费方是 cmd/core 的装配：
// 影子时用 ShadowDecider（恒放行），关闭后才用 director 的真实三值（INT-11）。
func (l *Loader) Shadow() bool { return l.shadow }

// SessionCookieName 返回业务自身的 session cookie 名（`session.cookie_name`）。
//
// 消费方是 `cmd/core` 的装配（`session.New(...)`）与适配器侧的 `SHEN_PROXY_SESSION_COOKIE`：
// **两侧必须是同一个名字**，否则适配器送来的 `session_hint` 会与核心的预期不同源 —— 启动日志会打印它。
func (l *Loader) SessionCookieName() string { return l.sessionCookie }

// Decoys 返回诱饵资产清单（数据）。消费方是 decoy 模块。
func (l *Loader) Decoys(context.Context) ([]contract.DecoyAsset, error) {
	out := make([]contract.DecoyAsset, len(l.decoys))
	copy(out, l.decoys)
	return out, nil
}

// Honeypots 返回幻境后端池（数据）。消费方是 honeypot 模块。
func (l *Loader) Honeypots(context.Context) ([]contract.HoneypotBackend, error) {
	out := make([]contract.HoneypotBackend, len(l.honeypots))
	copy(out, l.honeypots)
	return out, nil
}

// Injects 返回响应改写规则（`ST-24` 的数据）。
//
// 第二个返回值报告「配置里到底有没有这一段」：
//   - `provided=false`（未配置）→ 下发时**省略** `inject_rules` 字段，适配器继续用它自己的本地规则；
//   - `provided=true` 且列表为空 → 下发**显式空数组**，表示「没有规则」——运营可以据此主动关掉注入。
//
// 这个区分是刻意的：否则「没配」与「配了空」无法区分，运营就没有关闭注入的手段。
func (l *Loader) Injects(context.Context) ([]contract.InjectRule, bool, error) {
	if l.injects == nil {
		return nil, false, nil
	}
	out := make([]contract.InjectRule, len(*l.injects))
	copy(out, *l.injects)
	return out, true, nil
}

// Publish 把快照写入版本台账。
//
// 版本只增的校验由 store 侧强制（低于等于当前版本即拒绝）——
// 回滚不是回退版本号，而是发布一个内容为旧版、版本号更高的版本（AR-13 / ST-8）。
// 由装配层显式调用：只有 main 允许依赖具体实现。
func (l *Loader) Publish(ctx context.Context, s store.PolicyStore) error {
	if s == nil {
		return errors.New("policy: PolicyStore 不能为 nil")
	}
	if err := s.Publish(ctx, l.snap); err != nil {
		return fmt.Errorf("policy: 版本台账写入失败：%w", err)
	}
	return nil
}

// validate 逐节校验并把规则转成内部类型。
func (d *configDoc) validate() ([]contract.Rule, error) {
	if err := d.validateCoreAndSession(); err != nil {
		return nil, err
	}
	if d.Shadow == nil {
		return nil, missing("shadow")
	}
	// INT-11：任何接入的**首次上线必须**影子模式（默认 true，见 config.example.yaml）。
	// director（阶段 2a）交付后，「影子 → 接管」成为运维动作 —— 不在校验层强制，
	// 但关闭时启动日志必须点名（见 cmd/core）。
	if err := d.validateThresholds(); err != nil {
		return nil, err
	}
	if err := d.validateStore(); err != nil {
		return nil, err
	}
	if err := d.validatePolicy(); err != nil {
		return nil, err
	}
	rules, err := d.validateRules()
	if err != nil {
		return nil, err
	}
	if err := d.validateWhitelist(); err != nil {
		return nil, err
	}
	if err := d.validateDecoys(); err != nil {
		return nil, err
	}
	if err := d.validateHoneypots(); err != nil {
		return nil, err
	}
	if err := d.validateInjects(); err != nil {
		return nil, err
	}
	if err := d.validateAI(); err != nil {
		return nil, err
	}
	// 跨段一致性检查放在最后：它要同时看 decoys 与 honeypots 两段。
	if err := d.validateDecoyBackends(); err != nil {
		return nil, err
	}
	return rules, nil
}

// validateDecoyBackends 校验「启用中的诱饵 → 已登记且启用的幻境后端」这条跨段引用（C01）。
//
// 为什么必须跨段检查：`decoys.assets[].backend` 是一个**逻辑名**，它的有效定义在 `honeypots[]`。
// 只检查「非空」时，把后端名写错（或忘了启用它）会通过启动校验，然后在运行时表现为
// 「每个命中该诱饵的请求都拿到 502」—— 那是**部署未就绪却投放了线索**，
// 而 C01 的验收判据正是「部署未就绪不投放线索」。配置错就该在启动时失败（本项目的既有规矩）。
func (d *configDoc) validateDecoyBackends() error {
	if d.Decoys == nil || d.Decoys.Assets == nil {
		return nil
	}
	enabledBackends := map[string]bool{}
	if d.Honeypots != nil {
		for _, b := range *d.Honeypots {
			if b.Name != nil && b.Enabled != nil && *b.Enabled {
				enabledBackends[*b.Name] = true
			}
		}
	}
	for i := range *d.Decoys.Assets {
		a := (*d.Decoys.Assets)[i]
		if a.Enabled == nil || !*a.Enabled || a.Backend == nil {
			continue
		}
		if !enabledBackends[*a.Backend] {
			return fmt.Errorf(
				"policy: decoys.assets[%d].backend=%q 不是**已启用**的幻境后端 —— 先在 honeypots[] 里登记并启用它（C01：部署未就绪不投放线索）",
				i, *a.Backend)
		}
	}
	return nil
}

// decoyHosts 校验并归一化资产的归属声明（`W7`）。
//
// 规则（简单到能被审计）：
//   - 启用中的资产**必须**至少一条；未启用的可以留空（影子期只登记路径）；
//   - 形态只允许两种：精确主机名，或 `*.` 开头的前缀通配（禁止裸 `*` —— 那等于对所有主机宣示所有权）；
//   - 只允许字母/数字/连字符/点，长度 ≤ 253；统一小写，去掉尾点与端口。
func decoyHosts(prefix string, a decoyAssetDoc) ([]string, error) {
	enabled := a.Enabled != nil && *a.Enabled
	if a.Hosts == nil || len(*a.Hosts) == 0 {
		if enabled {
			return nil, fmt.Errorf(
				"policy: %s.hosts 不能为空 —— 启用中的诱饵资产必须声明它**拥有的主机名**（W7：路径归属要有主体，否则等于对所有主机宣示所有权）",
				prefix)
		}
		return nil, nil
	}
	out := make([]string, 0, len(*a.Hosts))
	seen := map[string]struct{}{}
	for i, raw := range *a.Hosts {
		h, err := normalizeHostPattern(raw)
		if err != nil {
			return nil, fmt.Errorf("policy: %s.hosts[%d]=%q %w", prefix, i, raw, err)
		}
		if _, dup := seen[h]; dup {
			return nil, fmt.Errorf("policy: %s.hosts[%d]=%q 重复", prefix, i, raw)
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	sort.Strings(out)
	return out, nil
}

// normalizeHostPattern 归一化一条主机声明（导出给适配器侧同一口径使用不必要 —— 那里只做匹配）。
func normalizeHostPattern(raw string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(raw))
	h = strings.TrimSuffix(h, ".")
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host // 允许写法里带端口，匹配时不看端口
	}
	if h == "" {
		return "", errors.New("为空")
	}
	if h == "*" {
		return "", errors.New("裸 * 不被接受（那是对所有主机宣示所有权）—— 用精确主机名或 *.domain 形态")
	}
	body := strings.TrimPrefix(h, "*.")
	if body == "" {
		return "", errors.New("通配缺少域名（应为 *.example.com）")
	}
	if strings.HasPrefix(h, "*.") {
		// 通配必须至少覆盖两级（`*.example.com` ✓ / `*.com` ✗）：后者等于宣告一个顶级域下的一切主机。
		if labels := strings.Count(body, "."); labels < 1 {
			return "", fmt.Errorf("通配过宽：%q 至少要写成 *.example.com（顶层域的一切主机不能归一个资产所有）", h)
		}
	}
	// 单级主机名（`localhost` / 内网短名）是**合法**的：它们正是内网部署里最常见的形态，
	// 而"归属声明"的价值在于**明确**，不在于层级多少 —— 过宽的形态由上面的通配规则挡住。
	if body == "" {
		return "", errors.New("空主机名")
	}
	if len(h) > 253 {
		return "", errors.New("过长（>253）")
	}
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.', r == '*':
		default:
			return "", fmt.Errorf("含非法字符 %q（只允许字母/数字/连字符/点，以及开头的 *.）", r)
		}
	}
	return h, nil
}

// validateInjects 校验响应改写规则段。该段**可选**（nil = 未配置）。
//
// 只查必填项与分类登记：片段不能为空、分类若写了必须是登记值。
// 标记（marker）允许为空（= 执行方用 `</body>`），因此不在这里补默认值 —— 默认值属于执行方。
func (d *configDoc) validateInjects() error {
	if d.Injects == nil {
		return nil
	}
	for i := range *d.Injects {
		r := (*d.Injects)[i]
		p := fmt.Sprintf("injects[%d]", i)
		if r.Snippet == nil || strings.TrimSpace(*r.Snippet) == "" {
			return fmt.Errorf("policy: %s.snippet 不能为空（空的注入片段没有意义）", p)
		}
		if r.Kind != nil && *r.Kind != "" && !slices.Contains(contract.InjectKinds, *r.Kind) {
			return fmt.Errorf("policy: %s.kind=%q 未登记（可用：%s）",
				p, *r.Kind, strings.Join(contract.InjectKinds, " / "))
		}
	}
	return nil
}

// validateDecoys 校验诱饵资产清单。该段**可选**（nil 表示未配置诱饵面）。
//
// 形态的合法值在这里校验；具体内容由 decoy 模块消费。
func (d *configDoc) validateDecoys() error {
	if d.Decoys == nil || d.Decoys.Assets == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(*d.Decoys.Assets))
	// 归属表：主机名（或通配后缀）-> 归一化路径 -> 资产 id。
	// 为什么按主机分区：同一条路径在**不同主机**上可以各归各的资产（多站点部署），
	// 但在同一主机上出现嵌套/重复归属就是配置错误 —— 匹配时谁赢取决于排序，那种"看运气"的语义不能要。
	owned := make(map[string]map[string]string, len(*d.Decoys.Assets))
	for i := range *d.Decoys.Assets {
		a := (*d.Decoys.Assets)[i]
		p := fmt.Sprintf("decoys.assets[%d]", i)
		if a.ID == nil || *a.ID == "" {
			return fmt.Errorf("policy: %s.id 不能为空", p)
		}
		if _, dup := seen[*a.ID]; dup {
			return fmt.Errorf("policy: %s.id=%q 重复 —— 资产 id 必须全文档唯一", p, *a.ID)
		}
		seen[*a.ID] = struct{}{}
		if a.Kind == nil {
			return missing(p + ".kind")
		}
		if _, ok := parseDecoyKind(*a.Kind); !ok {
			return fmt.Errorf("policy: %s.kind=%q 未知（允许：developer_api / instruction_file / mcp / dataset / bait）", p, *a.Kind)
		}
		if a.Path == nil || *a.Path == "" {
			return fmt.Errorf("policy: %s.path 不能为空", p)
		}
		if !strings.HasPrefix(*a.Path, "/") {
			return fmt.Errorf("policy: %s.path=%q 必须以 / 开头（诱饵路由是绝对路径）", p, *a.Path)
		}
		// 归一化形态是**发布条件**：匹配侧先归一化请求路径（方案 §9.2），
		// 资产侧若带着 `..` / 重复斜杠，两边口径就不一致，路由会时灵时不灵。
		if norm := contract.NormalizePath(*a.Path); norm != *a.Path {
			return fmt.Errorf("policy: %s.path=%q 不是归一化形态（应写作 %q）", p, *a.Path, norm)
		}
		if a.Enabled == nil {
			return missing(p + ".enabled")
		}
		// 归属声明（`W7`）：启用中的资产**必须**声明主机名。
		hosts, err := decoyHosts(p, a)
		if err != nil {
			return err
		}
		for _, h := range hosts {
			byPath, ok := owned[h]
			if !ok {
				byPath = map[string]string{}
				owned[h] = byPath
			}
			// 冲突发布**失败**：同一主机上的同一路由只能有一个资产（方案 C01 的验收判据），
			// 且不允许**嵌套归属**（`/admin` 与 `/admin/users` 同主机）—— 谁接管谁没有明确答案。
			for prevPath, prevID := range byPath {
				if prevPath == *a.Path {
					return fmt.Errorf("policy: %s.path=%q 在主机 %q 上与 assets[%s] 冲突 —— 同一路由只能登记一个资产",
						p, *a.Path, h, prevID)
				}
				if contract.PathSegmentPrefix(*a.Path, prevPath) || contract.PathSegmentPrefix(prevPath, *a.Path) {
					return fmt.Errorf(
						"policy: %s.path=%q 在主机 %q 上与 assets[%s] 的 %q 嵌套 —— 同一主机上的归属不得互相包含（匹配谁赢会变得不可预期）",
						p, *a.Path, h, prevID, prevPath)
				}
			}
			byPath[*a.Path] = *a.ID
		}
		// 「部署未就绪不投放线索」（C01 验收）：启用中的资产**必须**有后端。
		// 未启用时允许留空 —— 影子期先登记路径与内容，等后端就绪再打开（INT-11 的阶梯）。
		if *a.Enabled {
			if a.Backend == nil || strings.TrimSpace(*a.Backend) == "" {
				return fmt.Errorf("policy: %s.backend 不能为空 —— 启用中的诱饵必须指定投递到的幻境后端（C01）", p)
			}
		}
	}
	return nil
}

// validateHoneypots 校验幻境后端池。该段**可选**。
//
// 类型的**成员资格**由 honeypot 模块校验（避免 policy 反向依赖业务模块）；
// 这里只查必填项与唯一性。
func (d *configDoc) validateHoneypots() error {
	if d.Honeypots == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(*d.Honeypots))
	for i := range *d.Honeypots {
		h := (*d.Honeypots)[i]
		p := fmt.Sprintf("honeypots[%d]", i)
		if h.Name == nil || *h.Name == "" {
			return fmt.Errorf("policy: %s.name 不能为空", p)
		}
		if _, dup := seen[*h.Name]; dup {
			return fmt.Errorf("policy: %s.name=%q 重复 —— 逻辑名必须唯一", p, *h.Name)
		}
		seen[*h.Name] = struct{}{}
		if h.Type == nil || *h.Type == "" {
			return missing(p + ".type")
		}
		if h.Addr == nil || *h.Addr == "" {
			return fmt.Errorf("policy: %s.addr 不能为空", p)
		}
		if h.Enabled == nil {
			return missing(p + ".enabled")
		}
	}
	return nil
}

// parseDecoyKind 把配置里的形态名转成契约枚举。
func parseDecoyKind(s string) (contract.DecoyKind, bool) {
	switch s {
	case "developer_api":
		return contract.DecoyDeveloperAPI, true
	case "instruction_file":
		return contract.DecoyInstructionFile, true
	case "mcp":
		return contract.DecoyMCP, true
	case "dataset":
		return contract.DecoyDataset, true
	case "bait":
		return contract.DecoyBait, true
	default:
		return 0, false
	}
}

// buildDecoys 把配置转成诱饵资产（validate 已保证非 nil 字段安全）。
func (d *configDoc) buildDecoys() []contract.DecoyAsset {
	if d.Decoys == nil || d.Decoys.Assets == nil {
		return nil
	}
	out := make([]contract.DecoyAsset, 0, len(*d.Decoys.Assets))
	for i := range *d.Decoys.Assets {
		a := (*d.Decoys.Assets)[i]
		kind, _ := parseDecoyKind(*a.Kind)
		content, backend := "", ""
		if a.Content != nil {
			content = *a.Content
		}
		if a.Backend != nil {
			backend = strings.TrimSpace(*a.Backend)
		}
		hosts, _ := decoyHosts("", a) // validate 已保证形态合法
		out = append(out, contract.DecoyAsset{
			ID: *a.ID, Kind: kind, Path: *a.Path, Content: content, Backend: backend,
			Hosts: hosts, Enabled: *a.Enabled,
		})
	}
	return out
}

// buildInjects 把配置转成注入规则。**不排序**：注入按顺序执行，顺序是配置的一部分。
//
// `nil` 表示未配置（与「配置为空数组」不同，见 Injects）；分类不补默认值（空串 = 未分类）。
func (d *configDoc) buildInjects() *[]contract.InjectRule {
	if d.Injects == nil {
		return nil
	}
	out := make([]contract.InjectRule, 0, len(*d.Injects))
	for i := range *d.Injects {
		r := (*d.Injects)[i]
		kind := ""
		if r.Kind != nil {
			kind = *r.Kind
		}
		marker := ""
		if r.Marker != nil {
			marker = *r.Marker
		}
		out = append(out, contract.InjectRule{Kind: kind, Snippet: *r.Snippet, Marker: marker})
	}
	return &out
}

// buildHoneypots 把配置转成后端池。
//
// 新配置的后端初始健康 = 启用：否则池在首次健康检查前完全不可用。
// 即便假设有误，适配器在引流后端不可达时会回落业务（NI-5）。
func (d *configDoc) buildHoneypots() []contract.HoneypotBackend {
	if d.Honeypots == nil {
		return nil
	}
	out := make([]contract.HoneypotBackend, 0, len(*d.Honeypots))
	for i := range *d.Honeypots {
		h := (*d.Honeypots)[i]
		out = append(out, contract.HoneypotBackend{
			Name: *h.Name, Type: *h.Type, Addr: *h.Addr,
			Enabled: *h.Enabled, Healthy: *h.Enabled,
		})
	}
	return out
}

func (d *configDoc) validateCoreAndSession() error {
	if d.Core == nil {
		return missing("core")
	}
	if d.Core.Listen == nil {
		return missing("core.listen")
	}
	if _, _, err := net.SplitHostPort(*d.Core.Listen); err != nil {
		return fmt.Errorf("policy: core.listen=%q 不是合法的 host:port：%w", *d.Core.Listen, err)
	}
	if d.Session == nil {
		return missing("session")
	}
	if d.Session.CookieName == nil {
		return missing("session.cookie_name")
	}
	if *d.Session.CookieName == "" {
		return errors.New("policy: session.cookie_name 不能为空")
	}
	return nil
}

func (d *configDoc) validateThresholds() error {
	if d.Thresholds == nil {
		return missing("thresholds")
	}
	mirage, block, err := ratio("thresholds.route_mirage", d.Thresholds.RouteMirage, "thresholds.block", d.Thresholds.Block)
	if err != nil {
		return err
	}
	if mirage > block {
		return fmt.Errorf("policy: thresholds.route_mirage=%v 不得大于 thresholds.block=%v", mirage, block)
	}
	// `guard` 段**可选**：`false_route_budget`（误调度率护栏）本期不消费（阶段 2b+）⇒
	// 不该强制配置里必须写一个"写了也没用"的数。写了就照旧校验形态与取值范围，
	// 并会在核心启动日志被点名（`Loader.UnconsumedNotes`）—— 与 `store` 的未来驱动子段同一纪律。
	if d.Guard == nil {
		return nil
	}
	if _, _, err := ratio("guard.false_route_budget", d.Guard.FalseRouteBudget, "", nil); err != nil {
		return err
	}
	return nil
}

// ratio 校验一个（或两个）取值落在 [0,1] 的比例字段。
//
// aPath / bPath 用于生成**准确**的报错路径 —— 早前实现一律报「thresholds」，
// 配错 guard 时会把排障引向错误的字段。bPath 为空串表示只校验 a。
func ratio(aPath string, a *float64, bPath string, b *float64) (float64, float64, error) {
	if a == nil {
		return 0, 0, missing(aPath)
	}
	if *a < 0 || *a > 1 {
		return 0, 0, fmt.Errorf("policy: %s=%v 越界（要求 [0,1]）", aPath, *a)
	}
	if b == nil {
		return *a, 0, nil
	}
	if *b < 0 || *b > 1 {
		return *a, 0, fmt.Errorf("policy: %s=%v 越界（要求 [0,1]）", bPath, *b)
	}
	return *a, *b, nil
}

func (d *configDoc) validateStore() error {
	if d.Store == nil {
		return missing("store")
	}
	if d.Store.Driver == nil {
		return missing("store.driver")
	}
	if *d.Store.Driver != "memory" {
		return fmt.Errorf("policy: store.driver=%q 未实现（本期只支持 memory）", *d.Store.Driver)
	}
	// Redis / ClickHouse / Postgres 的子段**可选**（只在写了的时候校验形态）。
	//
	// 为什么改掉"必填"：driver 只接受 `memory` ⇒ 这三段**永远不被消费**，
	// 却原来要求配置里必须写出地址与密码 —— 那等于逼运维往一个**被忽略的字段**里填
	// 真实连接串（或填假值以骗过校验）。两种结果都糟：前者是凭据落在无用处，
	// 后者是"配置看起来齐全但全部无效"。改为可选后，想预留的人照样可以写（会被校验），
	// 而**写了什么会被启动日志点名**（见 `Loader.UnconsumedNotes`）。
	if d.Store.Redis != nil {
		if err := key(d.Store.Redis.Addr, "store.redis.addr"); err != nil {
			return err
		}
		if err := key(d.Store.Redis.Password, "store.redis.password"); err != nil {
			return err
		}
	}
	if d.Store.ClickHouse != nil {
		if err := key(d.Store.ClickHouse.Addr, "store.clickhouse.addr"); err != nil {
			return err
		}
		if err := key(d.Store.ClickHouse.Database, "store.clickhouse.database"); err != nil {
			return err
		}
	}
	if d.Store.Postgres != nil {
		if err := key(d.Store.Postgres.DSN, "store.postgres.dsn"); err != nil {
			return err
		}
	}
	return nil
}

// guardBudgetOf 取 `guard.false_route_budget`（**整段可省略** ⇒ 必须先判 nil）。
//
// ⚠️ 不能写成 `deref(d.Guard.FalseRouteBudget)`：当 `d.Guard == nil` 时**字段访问本身**就 panic
// （`deref` 只能兜住"字段指针为 nil"，兜不住"父结构体指针为 nil"）。这是把 `guard` 改成可选时
// 单测抓到的真实缺陷：合法配置（不写 guard）会把核心打崩。
func guardBudgetOf(g *guardDoc) float64 {
	if g == nil {
		return 0
	}
	return deref(g.FalseRouteBudget)
}

// deref 取指针值（nil ⇒ 零值）；用于"可选段"的读取。
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// UnconsumedNotes 列出**配置里写了、本期却不消费**的项（供启动日志点名）。
//
// 为什么要有它（配置合理性的一部分）：`core.listen` / `guard.false_route_budget` /
// `store.{redis,clickhouse,postgres}` 都在文档里写明"只校验不消费"，但**运维看不到文档就等于不知道** ——
// 于是有人改 `core.listen` 却没换 `SHEN_LISTEN`，或者把真实 Redis 密码填进一个被忽略的字段。
// 启动时把这些项**念一遍**，是"配置看起来生效其实没生效"这一类误解唯一的运行时出口。
func (l *Loader) UnconsumedNotes() []string {
	var out []string
	if l.coreListen != "" {
		out = append(out, fmt.Sprintf(
			"core.listen=%q（本期只校验不消费；实际监听地址取自环境变量 SHEN_LISTEN）", l.coreListen))
	}
	if l.guardBudget > 0 {
		out = append(out, fmt.Sprintf(
			"guard.false_route_budget=%g（本期只校验不消费；误调度率护栏属阶段 2b+）", l.guardBudget))
	}
	if l.storeExtra {
		out = append(out, "store.{redis,clickhouse,postgres}（本期只支持 driver=memory，"+
			"这些段不消费 —— 请勿在其中填写真实凭据）")
	}
	return out
}

func (d *configDoc) validatePolicy() error {
	if d.Policy == nil {
		return missing("policy")
	}
	if d.Policy.PolicyID == nil {
		return missing("policy.policy_id")
	}
	if *d.Policy.PolicyID == "" {
		return errors.New("policy: policy.policy_id 不能为空")
	}
	if d.Policy.Version == nil {
		return missing("policy.version")
	}
	if *d.Policy.Version < 1 {
		return fmt.Errorf("policy: policy.version=%d 非法（要求 ≥1）", *d.Policy.Version)
	}
	if d.Policy.GrayPct == nil {
		return missing("policy.gray_pct")
	}
	if *d.Policy.GrayPct < 0 || *d.Policy.GrayPct > 100 {
		return fmt.Errorf("policy: policy.gray_pct=%d 越界（要求 [0,100]）", *d.Policy.GrayPct)
	}
	return nil
}

func (d *configDoc) validateRules() ([]contract.Rule, error) {
	if d.Rules == nil {
		return nil, missing("rules")
	}
	// 空规则集合法（显式配空 ≠ 没配），但启动日志必须告警 —— 见 docs/spec/config.md §3.3。
	out := make([]contract.Rule, 0, len(*d.Rules))
	seen := make(map[string]struct{}, len(*d.Rules))
	for i := range *d.Rules {
		r := (*d.Rules)[i]
		p := fmt.Sprintf("rules[%d]", i)
		id, err := ruleID(r.ID, seen, p)
		if err != nil {
			return nil, err
		}
		if r.Weight == nil {
			return nil, missing(p + ".weight")
		}
		if *r.Weight <= 0 || *r.Weight > 1 {
			return nil, fmt.Errorf("policy: %s.weight=%v 越界（要求 0 < weight ≤ 1）", p, *r.Weight)
		}
		m, err := ruleMatch(r.Match, p)
		if err != nil {
			return nil, err
		}
		out = append(out, contract.Rule{ID: id, Weight: *r.Weight, Match: m})
	}
	return out, nil
}

func ruleID(v *string, seen map[string]struct{}, p string) (string, error) {
	if v == nil {
		return "", missing(p + ".id")
	}
	if *v == "" {
		return "", fmt.Errorf("policy: %s.id 不能为空", p)
	}
	if _, dup := seen[*v]; dup {
		return "", fmt.Errorf("policy: %s.id=%q 重复 —— 规则 id 必须全文档唯一", p, *v)
	}
	seen[*v] = struct{}{}
	return *v, nil
}

func ruleMatch(m *matchDoc, p string) (contract.Match, error) {
	if m == nil {
		return contract.Match{}, missing(p + ".match")
	}
	if m.Field == nil {
		return contract.Match{}, missing(p + ".match.field")
	}
	if !validField(*m.Field) {
		return contract.Match{}, fmt.Errorf("policy: %s.match.field=%q 未知（允许：user_agent / path / path_norm / query / query_raw / method / source_ip / tls_fingerprint）", p, *m.Field)
	}
	if m.Op == nil {
		return contract.Match{}, missing(p + ".match.op")
	}
	if !validOp(*m.Op) {
		return contract.Match{}, fmt.Errorf("policy: %s.match.op=%q 未知（允许：equals / prefix / path_prefix / contains）", p, *m.Op)
	}
	if m.Value == nil {
		return contract.Match{}, missing(p + ".match.value")
	}
	if *m.Value == "" {
		return contract.Match{}, fmt.Errorf("policy: %s.match.value 不能为空", p)
	}
	return contract.Match{Field: *m.Field, Op: *m.Op, Value: *m.Value}, nil
}

func (d *configDoc) validateWhitelist() error {
	if d.Whitelist == nil {
		return missing("whitelist")
	}
	// 本期只解析与校验、不消费：消费方是 director 的引流判定前置（INT-25）。
	if d.Whitelist.SourceCIDRs == nil {
		return missing("whitelist.source_cidrs")
	}
	for i, c := range *d.Whitelist.SourceCIDRs {
		if _, err := netip.ParsePrefix(c); err != nil {
			return fmt.Errorf("policy: whitelist.source_cidrs[%d]=%q 不是合法的 CIDR 前缀：%w", i, c, err)
		}
	}
	if d.Whitelist.UserAgents == nil {
		return missing("whitelist.user_agents")
	}
	if err := nonEmpty(*d.Whitelist.UserAgents, "whitelist.user_agents"); err != nil {
		return err
	}
	if d.Whitelist.PathPrefixes == nil {
		return missing("whitelist.path_prefixes")
	}
	if err := nonEmpty(*d.Whitelist.PathPrefixes, "whitelist.path_prefixes"); err != nil {
		return err
	}
	for i, p := range *d.Whitelist.PathPrefixes {
		if !strings.HasPrefix(p, "/") {
			return fmt.Errorf("policy: whitelist.path_prefixes[%d]=%q 必须以 / 开头", i, p)
		}
	}
	return nil
}

// build 构造快照：载荷只含 policy / rules / whitelist 三段。
//
// 密钥（store 段）与核心运行参数禁止进入载荷 —— 载荷会下发给 L1/L2/L3（ST-20）。
func (d *configDoc) build(rules []contract.Rule) (*Loader, error) {
	body := payloadDoc{
		Policy: payloadPolicy{
			PolicyID: *d.Policy.PolicyID,
			Version:  uint64(*d.Policy.Version),
			GrayPct:  uint8(*d.Policy.GrayPct),
		},
		Rules: rules,
		Whitelist: payloadWhitelist{
			SourceCIDRs:  nonNil(*d.Whitelist.SourceCIDRs),
			UserAgents:   nonNil(*d.Whitelist.UserAgents),
			PathPrefixes: nonNil(*d.Whitelist.PathPrefixes),
		},
	}
	// 键序固定（struct 字段序）、无多余空白 → 同内容必得同一校验和（docs/spec/config.md §3.2）。
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("policy: 序列化策略载荷失败：%w", err)
	}
	sum := sha256.Sum256(raw)
	return &Loader{
		snap: contract.PolicySnapshot{
			PolicyID: body.Policy.PolicyID,
			Version:  body.Policy.Version,
			Checksum: hex.EncodeToString(sum[:]),
			GrayPct:  body.Policy.GrayPct,
			Payload:  raw,
			Rules:    rules,
		},
		// validateThresholds 已保证两个指针非 nil，故此处安全解引用。
		thr: contract.Thresholds{
			Mirage: *d.Thresholds.RouteMirage,
			Block:  *d.Thresholds.Block,
		},
		shadow:        *d.Shadow,
		sessionCookie: *d.Session.CookieName,
		coreListen:    deref(d.Core.Listen),
		guardBudget:   guardBudgetOf(d.Guard),
		storeExtra:    d.Store.Redis != nil || d.Store.ClickHouse != nil || d.Store.Postgres != nil,
		ai:            d.buildAI(),
		decoys:        d.buildDecoys(),
		honeypots:     d.buildHoneypots(),
		wl:            d.buildWhitelist(),
		injects:       d.buildInjects(),
	}, nil
}

// payloadDoc 是策略载荷的线格式。字段顺序即键序，禁止随意调整。
type payloadDoc struct {
	Policy    payloadPolicy    `json:"policy"`
	Rules     []contract.Rule  `json:"rules"`
	Whitelist payloadWhitelist `json:"whitelist"`
}

type payloadPolicy struct {
	PolicyID string `json:"policy_id"`
	Version  uint64 `json:"version"`
	GrayPct  uint8  `json:"gray_pct"`
}

type payloadWhitelist struct {
	SourceCIDRs  []string `json:"source_cidrs"`
	UserAgents   []string `json:"user_agents"`
	PathPrefixes []string `json:"path_prefixes"`
}

func missing(path string) error {
	return fmt.Errorf("policy: 缺少必填项 %s", path)
}

func key(v *string, path string) error {
	if v == nil {
		return missing(path)
	}
	return nil
}

func nonEmpty(v []string, path string) error {
	for i, s := range v {
		if s == "" {
			return fmt.Errorf("policy: %s[%d] 不能为空", path, i)
		}
	}
	return nil
}

// nonNil 把空切片规范成空数组，使载荷序列化为 [] 而不是 null。
func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func validField(s string) bool {
	switch s {
	case "user_agent", "path", "path_norm", "query", "query_raw", "method", "source_ip", "tls_fingerprint":
		return true
	default:
		return false
	}
}

func validOp(s string) bool {
	switch s {
	case "equals", "prefix", "path_prefix", "contains":
		return true
	default:
		return false
	}
}

var _ Policy = (*Loader)(nil)
