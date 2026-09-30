package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"shen/common/core/internal/contract"
	"shen/common/core/internal/policy"
)

// 管控台同步（方案 B，见 docs/plan-console-deception-config.md）。
//
// 为什么放在 cmd/core（装配层）而不是 policy 包：policy 自我约束「不发起外呼、不依赖时钟」（MD-6），
// 而同步必然要做 HTTP、定时与本地缓存。policy 只提供纯函数 `Loader.WithOverlay`（复用 validate/build 做终检）。
//
// 失败语义（红线 ③）：管控台不可达 / 数据非法 ⇒ 保留 last-good 继续判定，并上报 applied=false；
// 重启时优先用本地缓存的 last-good（带校验和），其次部署配置。未配置 SHEN_CORE_CONSOLE_URL ⇒ 行为与今天逐字节一致。

const (
	versionStride     = 1_000_000 // 策略版本 = 部署配置 version × 1e6 + 投影修订号（重启不倒退）
	syncTimeout       = 3 * time.Second
	maxBackoff        = 2 * time.Minute
	maxProjectionBody = 8 << 20
)

// syncConfig 是同步的运行参数（全部来自环境变量）。
type syncConfig struct {
	URL       string
	Token     string
	Interval  time.Duration
	CachePath string
	// Note 非空 = 配了地址但同步被关闭的原因（启动日志点名；不阻断启动：业务按部署配置照常运行）。
	Note string
}

// loadSyncConfig 读环境变量；URL 为空 = 未启用。
func loadSyncConfig(getenv func(string) string) (syncConfig, error) {
	c := syncConfig{URL: strings.TrimRight(strings.TrimSpace(getenv("SHEN_CORE_CONSOLE_URL")), "/"),
		Interval: 15 * time.Second, CachePath: strings.TrimSpace(getenv("SHEN_CORE_SYNC_CACHE"))}
	if c.URL == "" {
		return c, nil
	}
	if !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://") {
		return c, fmt.Errorf("SHEN_CORE_CONSOLE_URL=%q 必须是 http(s):// 地址", c.URL)
	}
	tok := strings.TrimSpace(getenv("SHEN_CORE_CONSOLE_TOKEN"))
	if f := strings.TrimSpace(getenv("SHEN_CORE_CONSOLE_TOKEN_FILE")); f != "" {
		raw, err := os.ReadFile(f) // #nosec G304 -- 运维配置的 secrets 挂载路径
		if err != nil {
			return c, fmt.Errorf("读取 SHEN_CORE_CONSOLE_TOKEN_FILE 失败：%w", err)
		}
		tok = strings.TrimSpace(string(raw))
	}
	if tok == "" {
		c.Note = "已设置 SHEN_CORE_CONSOLE_URL=" + c.URL + " 但缺少 SHEN_CORE_CONSOLE_TOKEN ⇒ 管控台同步未启用（与管控台 SHEN_CONSOLE_CORE_SYNC_TOKEN 取同一值后生效）"
		c.URL = ""
		return c, nil
	}
	c.Token = tok
	if raw := strings.TrimSpace(getenv("SHEN_CORE_CONSOLE_SYNC_INTERVAL")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < 2*time.Second {
			return c, fmt.Errorf("SHEN_CORE_CONSOLE_SYNC_INTERVAL=%q 非法（要求 ≥2s 的时长，如 15s）", raw)
		}
		c.Interval = d
	}
	return c, nil
}

// pullResponse 是管控台 GET /api/v1/integration/deception 的响应。
type pullResponse struct {
	State      string `json:"state"` // ok | not_initialized | invalid
	Projection struct {
		Rev    uint64 `json:"rev"`
		Digest string `json:"digest"`
		policy.Overlay
	} `json:"projection"`
	Errors []struct {
		Field  string `json:"field"`
		Reason string `json:"reason"`
	} `json:"errors"`
}

// consoleReport 是每轮上报（与管控台 deception.CoreReport 手工对齐）。
type consoleReport struct {
	AppliedRev    uint64        `json:"applied_rev"`
	PolicyVersion uint64        `json:"policy_version"`
	BaseVersion   int64         `json:"base_version"`
	Applied       bool          `json:"applied"`
	Reason        string        `json:"reason,omitempty"`
	Source        string        `json:"source"`
	EdgeAcks      []reportAck   `json:"edge_acks"`
	Honeypots     []probeResult `json:"honeypots"`
}

type reportAck struct {
	AdapterID  string    `json:"adapter_id"`
	Version    uint64    `json:"version"`
	Applied    bool      `json:"applied"`
	Reason     string    `json:"reason,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}

// cacheFile 是本地 last-good 缓存（带校验和：损坏即忽略，回到部署配置）。
type cacheFile struct {
	Rev      uint64         `json:"rev"`
	Digest   string         `json:"digest"`
	Overlay  policy.Overlay `json:"overlay"`
	Checksum string         `json:"checksum"`
}

// consoleSync 是同步状态机（单 goroutine 驱动；状态读写经 mu，供上报与测试读取）。
type consoleSync struct {
	cfg     syncConfig
	base    *policy.Loader
	apply   func(*policy.Loader) error
	acks    func(context.Context) ([]contract.PolicyAck, error)
	probes  func() []probeResult
	client  *http.Client
	logf    func(string, ...any)
	mu      sync.Mutex
	seenRev uint64 // 最近处理过的修订号（应用成功或被拒）：since_rev 用它，避免反复终检同一份坏数据
	state   consoleReport
	lastLog string
}

func newConsoleSync(cfg syncConfig, base *policy.Loader, apply func(*policy.Loader) error,
	acks func(context.Context) ([]contract.PolicyAck, error), probes func() []probeResult) *consoleSync {
	return &consoleSync{cfg: cfg, base: base, apply: apply, acks: acks, probes: probes,
		client: &http.Client{Timeout: syncTimeout}, logf: log.Printf,
		state: consoleReport{Applied: true, Source: "file", BaseVersion: base.BaseVersion(),
			PolicyVersion: uint64(base.BaseVersion())}}
}

// versionFor 计算覆盖后的策略版本。
func (c *consoleSync) versionFor(rev uint64) int64 {
	return c.base.BaseVersion()*versionStride + int64(rev)
}

// restoreCache 在启动时尝试用本地缓存的 last-good 接管（管控台暂时不可达时业务仍用最新配置）。
func (c *consoleSync) restoreCache() {
	if c.cfg.CachePath == "" {
		return
	}
	raw, err := os.ReadFile(c.cfg.CachePath)
	if err != nil {
		if !os.IsNotExist(err) {
			c.logf("WARN 读取同步缓存 %s 失败：%v（回到部署配置）", c.cfg.CachePath, err)
		}
		return
	}
	var f cacheFile
	if err := json.Unmarshal(raw, &f); err != nil || f.Checksum != overlaySum(f.Overlay) {
		c.logf("WARN 同步缓存 %s 损坏或校验和不符，已忽略（回到部署配置）", c.cfg.CachePath)
		return
	}
	if err := c.accept(f.Rev, f.Overlay, "cache"); err != nil {
		c.logf("WARN 同步缓存 r%d 未通过终检，已忽略：%v", f.Rev, err)
		return
	}
	c.logf("管控台同步：已用本地缓存 r%d 接管（等待管控台连通后校准）", f.Rev)
}

// accept 终检并应用一份覆盖数据（成功才更新状态）。
func (c *consoleSync) accept(rev uint64, o policy.Overlay, source string) error {
	next, err := c.base.WithOverlay(o, c.versionFor(rev))
	if err != nil {
		return err
	}
	if err := c.apply(next); err != nil {
		return err
	}
	c.mu.Lock()
	c.seenRev = rev
	c.state.AppliedRev, c.state.Applied, c.state.Reason, c.state.Source = rev, true, "", source
	c.state.PolicyVersion = uint64(c.versionFor(rev))
	c.mu.Unlock()
	return nil
}

// run 驱动同步循环，直到 ctx 结束。失败按指数退避（上限 2min）加抖动。
func (c *consoleSync) run(ctx context.Context) {
	c.restoreCache()
	failures := 0
	for {
		err := c.once(ctx)
		c.report(ctx)
		wait := c.cfg.Interval
		if err != nil {
			failures++
			wait = min(c.cfg.Interval<<min(failures, 6), maxBackoff)
			c.note("WARN 管控台同步失败（第 %d 次，%s 后重试；核心继续用 last-good）：%v", failures, wait.Round(time.Second), err)
		} else if failures > 0 {
			failures = 0
			c.note("管控台同步已恢复")
		}
		wait += time.Duration(rand.Int64N(int64(wait/5) + 1)) // #nosec G404 -- 抖动，非安全用途
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// once 执行一次有条件拉取；返回的错误只代表「通道不通」（数据非法不算，那是 applied=false）。
func (c *consoleSync) once(ctx context.Context) error {
	c.mu.Lock()
	since := c.seenRev
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	url := c.cfg.URL + "/api/v1/integration/deception?since_rev=" + strconv.FormatUint(since, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return nil
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("管控台返回 HTTP %d", resp.StatusCode)
	}
	var body pullResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxProjectionBody)).Decode(&body); err != nil {
		return fmt.Errorf("解析管控台响应失败：%w", err)
	}
	switch body.State {
	case "not_initialized":
		c.reject(0, "管控台数据集尚未初始化：沿用当前配置", true)
		c.note("管控台数据集未初始化：核心继续使用当前配置")
		return nil
	case "invalid":
		reasons := make([]string, 0, len(body.Errors))
		for _, e := range body.Errors {
			reasons = append(reasons, e.Field+" "+e.Reason)
		}
		c.reject(0, "管控台数据集当前不合法："+strings.Join(reasons, "；"), false)
		c.note("WARN 管控台数据集当前不合法，保留 last-good：%s", strings.Join(reasons, "；"))
		return nil
	case "ok":
	default:
		return fmt.Errorf("管控台返回未知状态 %q", body.State)
	}
	rev := body.Projection.Rev
	if rev == 0 || rev >= versionStride {
		c.reject(rev, fmt.Sprintf("投影修订号 r%d 越界", rev), false)
		return nil
	}
	if err := c.accept(rev, body.Projection.Overlay, "console"); err != nil {
		c.reject(rev, err.Error(), false)
		c.note("WARN 拒绝管控台下发的 r%d（保留 last-good）：%v", rev, err)
		return nil
	}
	c.note("管控台同步：已应用 r%d（策略版本 v%d）", rev, c.versionFor(rev))
	c.writeCache(rev, body.Projection.Digest, body.Projection.Overlay)
	return nil
}

// reject 记下一次未应用；keepApplied=true 表示「没有新数据」而不是「数据被拒」。
func (c *consoleSync) reject(rev uint64, reason string, keepApplied bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if rev > 0 {
		c.seenRev = rev
	}
	c.state.Applied, c.state.Reason = keepApplied, reason
}

// note 只在消息变化时写日志（防刷屏：同一状态每 15s 一行没有信息量）。
func (c *consoleSync) note(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	c.mu.Lock()
	same := msg == c.lastLog
	c.lastLog = msg
	c.mu.Unlock()
	if !same {
		c.logf("%s", msg)
	}
}

func (c *consoleSync) snapshot(ctx context.Context) consoleReport {
	c.mu.Lock()
	r := c.state
	c.mu.Unlock()
	r.EdgeAcks = []reportAck{}
	if acks, err := c.acks(ctx); err == nil {
		r.EdgeAcks = latestAcks(acks)
	}
	r.Honeypots = []probeResult{}
	if c.probes != nil {
		r.Honeypots = c.probes()
	}
	return r
}

// report 把本轮状态 POST 给管控台（失败只记日志：上报是观测，不影响判定）。
func (c *consoleSync) report(ctx context.Context) {
	raw, err := json.Marshal(c.snapshot(ctx))
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL+"/api/v1/integration/deception/report", bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// latestAcks 每个适配器只上报版本最高的一条回执（台账里同一适配器会有多个版本）。
func latestAcks(in []contract.PolicyAck) []reportAck {
	best := map[string]contract.PolicyAck{}
	for _, a := range in {
		if cur, ok := best[a.AdapterID]; !ok || a.Version > cur.Version ||
			(a.Version == cur.Version && a.ReceivedAt.After(cur.ReceivedAt)) {
			best[a.AdapterID] = a
		}
	}
	out := make([]reportAck, 0, len(best))
	for _, a := range best {
		out = append(out, reportAck{AdapterID: a.AdapterID, Version: a.Version, Applied: a.Applied,
			Reason: a.Reason, ReceivedAt: a.ReceivedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AdapterID < out[j].AdapterID })
	return out
}

// writeCache 原子写本地缓存（临时文件 + rename）；失败只告警。
func (c *consoleSync) writeCache(rev uint64, digest string, o policy.Overlay) {
	if c.cfg.CachePath == "" {
		return
	}
	raw, err := json.Marshal(cacheFile{Rev: rev, Digest: digest, Overlay: o, Checksum: overlaySum(o)})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.cfg.CachePath), 0o700); err != nil {
		c.logf("WARN 创建同步缓存目录失败：%v", err)
		return
	}
	tmp := c.cfg.CachePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		c.logf("WARN 写同步缓存失败：%v", err)
		return
	}
	if err := os.Rename(tmp, c.cfg.CachePath); err != nil {
		c.logf("WARN 写同步缓存失败：%v", err)
	}
}

func overlaySum(o policy.Overlay) string {
	raw, _ := json.Marshal(o)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
