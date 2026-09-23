// Package mirror 是接入形态①（旁路镜像）的接收端。
//
// 它**不在请求路径上**：只消费流量的一份副本，不产生任何出站连接
// （除了把观测与遥测送给核心）。
//
// 设计边界：本包只允许使用 api/ 生成的 proto 类型，**不得** import
// core/internal/ —— 这是 Go 的 internal 目录规则在编译期强制的。
package mirror

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	judgev1 "shen/common/api/judge/v1"
	telemetryv1 "shen/common/api/telemetry/v1"
)

// JudgeClient 是本端对核心判定面的依赖。
// 生成的 DeceptionJudgeClient 天然满足它；单测用替身。
type JudgeClient interface {
	Judge(ctx context.Context, in *judgev1.JudgeRequest, opts ...grpc.CallOption) (*judgev1.JudgeResponse, error)
}

// TelemetryClient 是本端对核心遥测面的依赖。
type TelemetryClient interface {
	Report(ctx context.Context, in *telemetryv1.TelemetryEvent, opts ...grpc.CallOption) (*telemetryv1.ReportAck, error)
}

// Receiver 把镜像到的 HTTP 请求转成一次判定请求，并把结果记为事件。
//
// 它永远不让调用方失败：镜像形态下我们不在业务路径上，
// 所以无论内部发生什么，都对镜像源返回 202。
type Receiver struct {
	Judge  JudgeClient
	Report TelemetryClient
	Now    func() time.Time // 注入时钟，使 decision_id 可复现、可测
	Window time.Duration    // decision_id 的时间窗；同一窗口内同请求得同一 ID
	// TrustXFF 是否信任 X-Forwarded-For 取客户端 IP。
	TrustXFF bool
	// SessionCookie 是业务自身的 session cookie 名（**必须与核心的 `session.cookie_name` 一致**）。
	// 空 = 默认 `sid`。它决定「最小会话身份」怎么取（见 sessionHint）。
	SessionCookie string
}

const (
	defaultWindow = 60 * time.Second
	judgeTimeout  = 3 * time.Second
	// DefaultSessionCookie 是默认的 session cookie 名（与核心 `session.cookie_name` 的示例值一致）。
	DefaultSessionCookie = "sid"
)

// sessionCookieName 返回生效的 cookie 名（空 = 默认值）。
func (r *Receiver) sessionCookieName() string {
	if v := strings.TrimSpace(r.SessionCookie); v != "" {
		return v
	}
	return DefaultSessionCookie
}

// sessionHint 按约定的 cookie 名取出**最小会话身份值**（不是整段 Cookie 头，方案 §5.2）。
//
// 与形态③④（`deception/proxy`）的 `sessionHint` **同语义、各自实现**（适配器必须能独立部署，`INT-5`）；
// 解析口径与核心的 `session.cookieValue` 一致（`;` 分隔、取第一个同名项、名字两侧空白不宽容）。
func sessionHint(cookieHeader, cookieName string) string {
	if strings.TrimSpace(cookieName) == "" {
		return ""
	}
	for _, part := range strings.Split(cookieHeader, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && k == cookieName {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (r *Receiver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Receiver) window() time.Duration {
	if r.Window > 0 {
		return r.Window
	}
	return defaultWindow
}

// ServeHTTP 处理一份镜像副本。
//
// 顺序：取观测 → 算 decision_id → 请核心判定 → 记事件 → 202。
// 任何一步出错都不向外抛：镜像源只关心副本被消费掉了。
func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// 给调核心一个上限：核心挂死不该把镜像源的连接一直占住。
	// 镜像**不在业务路径上**，所以这里不需要 3ms 级硬超时；3s 足够，
	// 超时后照常返回 202 并记一条「判定缺失」事件。
	ctx, cancel := context.WithTimeout(req.Context(), judgeTimeout)
	defer cancel()

	obs := r.observationFrom(req)
	reqProto := &judgev1.JudgeRequest{
		DecisionId: decisionID(obs, r.window(), r.now()),
		Observed:   obs,
	}

	resp, err := r.Judge.Judge(ctx, reqProto)
	if err != nil {
		// 核心不可用不影响业务：只记下"判定缺失"这件事。
		resp = &judgev1.JudgeResponse{}
	}

	_ = r.reportEvent(ctx, reqProto, resp)

	w.WriteHeader(http.StatusAccepted)
}

// observationFrom 把 HTTP 请求转成观测。
//
// 注意：这里只做"读"，不做任何判定 —— 判定是核心的职责。
func (r *Receiver) observationFrom(req *http.Request) *judgev1.Observation {
	headers := make(map[string]string, len(req.Header))
	for k, v := range req.Header {
		// Cookie 头**不跨接缝**：核心只需要「按约定名字取出的最小身份」（session_hint）。
		if len(v) > 0 && !strings.EqualFold(k, "Cookie") {
			headers[lower(k)] = v[0]
		}
	}

	// 刻意不读请求体：判定面契约（api/judge/v1 的 Observation）没有 body 字段，
	// 阈值校准用头与路径。读 body 塞进 header 是无人消费的死数据（此前的 x-observed-body-prefix 无下游）。
	// 误导处置需要读 body（INT-22）是阶段 2b 的事，届时契约演进顺带加 body 字段。

	return &judgev1.Observation{
		SourceIp:    r.clientIP(req),
		UserAgent:   req.Header.Get("User-Agent"),
		Method:      req.Method,
		Path:        req.URL.Path,
		Query:       queryOf(req),
		QueryRaw:    rawQueryOf(req),
		SessionHint: sessionHint(req.Header.Get("Cookie"), r.sessionCookieName()),
		Headers:     headers,
	}
}

// maxDecodeRounds 是查询串规范化的解码轮数上限（同形态③④：解到不动点，最多 3 轮）。
const maxDecodeRounds = 3

// queryOf 取查询串的**规范化匹配视图**（不含前导 `?`）：反复解码到不动点，上限 `maxDecodeRounds` 轮。
//
// 与形态③/④（`deception/proxy`）的 `queryOf` **同语义、各自实现**：适配器之间必须能独立部署（`INT-5`），
// 两处刻意不共享代码 —— 改其一时必须同时改另一处。
// 约定：解到不动点（`%252e` → `%2e` → `.`；`+`/`%20` → 空格）· 有上限 · 失败时原样传递（`AR-31`）。
// 它只用于匹配：原样字节见 `rawQueryOf`。
func queryOf(req *http.Request) string {
	cur := req.URL.RawQuery
	// 尝试 maxDecodeRounds **次**（不是「轮数 + 1」）：这样常量就是字面意思 ——
	// 三层编码（`%25252e`）刚好解满，四层及以上停下来（成本钉死，已登记）。
	for attempt := 0; attempt < maxDecodeRounds; attempt++ {
		next, err := url.QueryUnescape(cur)
		if err != nil || next == cur {
			break
		}
		cur = next
	}
	return cur
}

// rawQueryOf 取**原样**查询串（未解码），供审计与「按编码形态匹配」的规则使用（`AR-31`）。
func rawQueryOf(req *http.Request) string { return req.URL.RawQuery }

func (r *Receiver) clientIP(req *http.Request) string {
	if r.TrustXFF {
		if v := req.Header.Get("X-Forwarded-For"); v != "" {
			// XFF 是逗号分隔列表，第一段是原始客户端。
			for i := 0; i < len(v); i++ {
				if v[i] == ',' {
					return strings.TrimSpace(v[:i])
				}
			}
			return strings.TrimSpace(v)
		}
	}
	host := req.RemoteAddr
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return strings.TrimSpace(host[:i])
		}
	}
	return strings.TrimSpace(host)
}

// decisionID 按（来源标识, 会话, 路径, 时间窗）派生，同一组合得到同一 ID，
// 以便核心侧按这个键做幂等与去重。
//
// 「会话」这里取**最小身份值**（`session_hint`：按约定的 cookie 名取出的那一个值）——
// 整段 Cookie 头既不进 ID，也不跨接缝（方案 §5.2）。
func decisionID(obs *judgev1.Observation, window time.Duration, now time.Time) string {
	key := strings.Join([]string{
		obs.GetSourceIp(),
		obs.GetSessionHint(),
		obs.GetPath(),
		strconv.FormatInt(now.Truncate(window).Unix(), 10),
	}, "\x00")
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16])
}

func (r *Receiver) reportEvent(ctx context.Context, req *judgev1.JudgeRequest, resp *judgev1.JudgeResponse) error {
	ev := &telemetryv1.TelemetryEvent{
		EventId:   req.GetDecisionId(),
		EventType: "request_observed",
		// 会话标识用**最小身份值**：此前这里是整段 Cookie 头，等于把认证材料写进遥测库（方案 §5.2）。
		SessionId: req.GetObserved().GetSessionHint(),
		Payload:   payloadBytes(req.GetObserved(), resp),
		CreatedAt: timestamppb.New(r.now()),
	}
	_, err := r.Report.Report(ctx, ev)
	return err
}

// payloadBytes 把本次观测与判定结果打包成事件载荷。
//
// 只放阀值校准真正用得上的字段，**不放请求体全量** ——
// 事件表按天分区，载荷过大会很快撑爆存储。
func payloadBytes(obs *judgev1.Observation, resp *judgev1.JudgeResponse) []byte {
	p := map[string]any{
		"method":   obs.GetMethod(),
		"path":     obs.GetPath(),
		"ua":       obs.GetUserAgent(),
		"action":   resp.GetAction().String(),
		"severity": resp.GetSeverity().String(),
	}
	if b := resp.GetBackend(); b != "" {
		p["backend"] = b
	}
	out, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	return out
}

// lower 归一化头名：HTTP 头名大小写不敏感，统一成小写便于核心侧稳定取值。
func lower(s string) string { return strings.ToLower(s) }
