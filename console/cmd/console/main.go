// Command console 是**观测控制台**：把核心的观测面读出来，用一页网页展示
// 「告警在哪、流量访问长什么样、每个请求在引擎里怎么流动」。
//
// 为什么要有它（本项目的目标之一）：在**人工测试**阶段，最耗时的不是跑起来，
// 而是「跑起来之后看不见发生了什么」—— 判定分数、命中信号、决策去向散在日志里。
// 这一页把它们摆在一起，且**不需要任何前端工具链**（纯静态 HTML + 原生 fetch）。
//
// 边界（不做的事）：
//   - **只读**：不写策略、不改配置、不干预请求（控制面能力见 `console` 模块文档的「不做什么」）；
//   - **不参与请求级判定**（`AR-10`）：它是旁观者，页面挂了不影响业务；
//   - 不引入构建步骤：页面是静态资源，由本进程直接服务。
//
// 依据：`docs/modules/console.md` · `AR-10`（控制面不参与判定）· `ST-7`（判定细节只进观测面，不回客户端）。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "shen/api/telemetry/v1"
	"shen/console/internal/topology"
	"shen/console/web"
)

// 默认地址：核心在同机监听 127.0.0.1:9443；控制台自己监听 127.0.0.1:9444。
const (
	defaultAlertScore = 0.9 // 「高风险」显示阈值：只影响图的标色，不改变任何处置

	defaultCoreAddr = "127.0.0.1:9443"
	defaultListen   = "127.0.0.1:9444"
	// decisionEventType 是核心写侧用的判定事件类型（见 core/cmd/core 的观测面适配器）。
	decisionEventType = "decision"

	// analysisEventType 是 L4 近线分析上报的结论事件类型（见 analysis/worker.py）。
	analysisEventType = "analysis"

	// judgedEventType 是适配器上报的逐请求执行事件（含实际落点与返回信息）。
	judgedEventType = "request_judged"
)

// flowRecord 是**判定事件**的载荷形状（跨进程契约：与核心写侧的 DecisionRecord 字段一一对应）。
//
// 两端分属不同平面，因此没有共享的 Go 类型（适配器禁止 import 核心内部包）——
// 改动其一**必须**同时改另一处，并在模块文档里记一笔。
// flowRecord 与核心写出的事件载荷**同一形状**（snake_case）——
// 页面的接口也用它，避免"载荷一套键、接口另一套键"的漂移。
type flowRecord struct {
	DecisionID string    `json:"decision_id"`
	SourceIP   string    `json:"source_ip"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	UserAgent  string    `json:"user_agent"`
	Action     string    `json:"action"`
	Severity   string    `json:"severity"`
	Backend    string    `json:"backend"`
	Score      float64   `json:"score"`
	Signals    []string  `json:"signals"`
	At         time.Time `json:"at"`
}

// eventView 是页面消费的事件视图（把 payload 解成对象，前端不用再解一次 JSON 字符串）。
type eventView struct {
	EventID   string          `json:"event_id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	ActorID   string          `json:"actor_id"`
	SessionID string          `json:"session_id"`
	Flow      *flowRecord     `json:"flow,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

// summary 是页头概览。
// analysisView 是 L4 结论事件的页面视图。
//
// 结论只是**数据**：控制台只读展示，不改策略、不干预请求（AR-10 / AR-32）。
type analysisView struct {
	EventID     string          `json:"event_id"`
	At          time.Time       `json:"at"`
	Kind        string          `json:"kind"`
	Accepted    bool            `json:"accepted"`
	Data        json.RawMessage `json:"data,omitempty"`
	Reason      string          `json:"rejected_reason,omitempty"`
	Analyzed    int             `json:"analyzed"`
	EvidenceIDs []string        `json:"evidence_ids,omitempty"`
}

type summary struct {
	Total    int            `json:"total"`
	ByAction map[string]int `json:"by_action"`
	Alerts   int            `json:"alerts"` // block 或 severity 非 none 的条数
	// L4Conclusions 是 L4 近线分析上报的结论条数（页面「分析结论」块的数据源）
	L4Conclusions int        `json:"l4_conclusions"`
	FirstSeen     *time.Time `json:"first_seen,omitempty"`
	LastSeen      *time.Time `json:"last_seen,omitempty"`
}

type server struct {
	client telemetryv1.DeceptionTelemetryClient
}

func main() {
	coreAddr := flag.String("core", env("SHEN_CORE_ADDR", defaultCoreAddr), "核心 gRPC 地址")
	listen := flag.String("listen", env("SHEN_CONSOLE_LISTEN", defaultListen), "控制台监听地址")
	flag.Parse()

	// 与核心之间走本机明文 gRPC（同机部署；跨节点要换 mTLS —— 与本项目其他接缝同一约定）。
	conn, err := grpc.NewClient(*coreAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("console: 连接核心失败：%v", err)
	}
	defer func() { _ = conn.Close() }()

	s := &server{client: telemetryv1.NewDeceptionTelemetryClient(conn)}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/summary", s.handleSummary)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/stream", s.handleStream)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/flow", s.handleFlow)
	mux.HandleFunc("/api/analysis", s.handleAnalysis)
	mux.HandleFunc("/api/topology", s.handleTopology)
	mux.HandleFunc("/api/trace", s.handleTrace)
	mux.HandleFunc("/api/graphs", s.handleGraphs)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		// 存活探针只反映**本进程**存活；核心是否可达由页面上的错误提示体现（ST-17 的语义区分）。
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("console 已启动：监听 %s，核心 %s（打开 http://%s/ 查看）", *listen, *coreAddr, *listen)
	srv := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("console: %v", err)
	}
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(web.IndexHTML)
}

// handleEvents 返回最近事件（原始视图：谁在什么时候发了什么事件）。
func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.fetch(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, events)
}

// handleFlow 只返回**判定事件**，并把载荷解成结构化字段 —— 这是页面「流量流动」表的数据源。
//
// 取数时**显式只要 decision 事件**：否则 limit 会先作用在全部事件上（含 request_judged），
// 过滤后行数会远少于 limit —— 页面看起来"记录变少了"，其实是被别的类型挤掉了。
func (s *server) handleFlow(w http.ResponseWriter, r *http.Request) {
	events, err := s.fetchType(r, decisionEventType)
	if err != nil {
		writeErr(w, err)
		return
	}
	// 用 make(...,0) 而不是 var：Go 把 nil 切片序列化成 null，
	// 客户端就得同时处理 null 与 []（真踩过：验证脚本读 /api/flow 拿到 null 直接报错）。
	out := make([]flowRecord, 0, len(events))
	for _, ev := range events {
		if ev.Flow != nil {
			out = append(out, *ev.Flow)
		}
	}
	writeJSON(w, out)
}

// handleAnalysis 只返回 L4 的结论事件，并把载荷解成结构化字段。
//
// 这是「L4 到底分析了什么」在页面上的出处（人工测试时不用去翻日志）。
func (s *server) handleAnalysis(w http.ResponseWriter, r *http.Request) {
	events, err := s.fetchType(r, analysisEventType)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]analysisView, 0, len(events)) // 同上：空列表也必须是 []
	for _, ev := range events {
		var payload struct {
			Kind        string          `json:"kind"`
			Accepted    bool            `json:"accepted"`
			Data        json.RawMessage `json:"data"`
			Reason      string          `json:"rejected_reason"`
			Analyzed    int             `json:"analyzed"`
			EvidenceIDs []string        `json:"evidence_ids"`
		}
		if err := json.Unmarshal(ev.Raw, &payload); err != nil {
			continue // 解不出的结论不进页面：不猜、不补默认值
		}
		out = append(out, analysisView{
			EventID:     ev.EventID,
			At:          ev.CreatedAt,
			Kind:        payload.Kind,
			Accepted:    payload.Accepted,
			Data:        payload.Data,
			Reason:      payload.Reason,
			Analyzed:    payload.Analyzed,
			EvidenceIDs: payload.EvidenceIDs,
		})
	}
	writeJSON(w, out)
}

// fetchObservations 取构建链路/图所需的**三类事件**。
//
// 为什么按类型分别取：一次请求产生两条事件（适配器的 request_judged 与核心的 decision），
// 若按总条数取（limit），同一批事件可能只有一半落在窗口里 ⇒ 图上"意图"（核心判定）会整列缺失。
func (s *server) fetchObservations(r *http.Request) (topology.Input, error) {
	in := topology.Input{}
	judged, err := s.fetchType(r, judgedEventType)
	if err != nil {
		return in, err
	}
	for _, ev := range judged {
		var j topology.JudgedEvent
		if json.Unmarshal(ev.Raw, &j) != nil {
			continue
		}
		if j.DecisionID == "" {
			j.DecisionID = ev.EventID // 旧数据的兼容路径（事件 id 曾是 decision_id）
		}
		j.At = ev.CreatedAt
		in.Judged = append(in.Judged, j)
	}

	decisions, err := s.fetchType(r, decisionEventType)
	if err != nil {
		return in, err
	}
	for _, ev := range decisions {
		var d topology.DecisionEvent
		if json.Unmarshal(ev.Raw, &d) == nil && d.DecisionID != "" {
			in.Decisions = append(in.Decisions, d)
		}
	}

	analysis, err := s.fetchType(r, analysisEventType)
	if err != nil {
		return in, err
	}
	for _, ev := range analysis {
		var a topology.AnalysisEvent
		if json.Unmarshal(ev.Raw, &a) == nil {
			a.At = ev.CreatedAt
			in.Analysis = append(in.Analysis, a)
		}
	}
	return in, nil
}

// handleTopology 把观测面事件聚合成**流量调度图**（只读；AR-10：控制面不参与判定）。
//
// 数据来源：核心的 decision 事件（意图：分值/信号/决策）+ 适配器的 request_judged 事件
// （实际落点与返回信息）+ L4 的 analysis 事件（注解）。三者以 decision_id 关联。
func (s *server) handleTopology(w http.ResponseWriter, r *http.Request) {
	in, err := s.fetchObservations(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, topology.Build(in, alertScore()))
}

// handleGraphs 返回**逐请求链路**：每个请求一条独立 DAG（不聚合），最新的在前。
// 页面每 5 秒重新取一次，因此新流量会动态出现在最上面。
func (s *server) handleGraphs(w http.ResponseWriter, r *http.Request) {
	in, err := s.fetchObservations(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, topology.BuildRequests(in, alertScore()))
}

// traceView 是单条请求的完整链路（页面的详情面板用它）。
type traceView struct {
	DecisionID string            `json:"decision_id"`
	Judged     json.RawMessage   `json:"judged,omitempty"`   // 适配器：实际落点 + 返回信息
	Decision   json.RawMessage   `json:"decision,omitempty"` // 核心：意图（分值 / 信号 / 决策）
	Analysis   []json.RawMessage `json:"analysis,omitempty"` // L4 注解（引用到这条判定的结论）
	Alerts     traceAlerts       `json:"alerts"`
	Notes      []string          `json:"notes"`
}

type traceAlerts struct {
	Real     bool   `json:"real"`      // 现行口径：拦截 或 severity≠none
	HighRisk bool   `json:"high_risk"` // 仅显示用：score ≥ 阈值
	Reason   string `json:"reason"`
}

// handleTrace 返回某条 decision_id 的完整链路。
func (s *server) handleTrace(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("decision_id"))
	if id == "" {
		writeErr(w, errors.New("缺 decision_id 参数"))
		return
	}
	events, err := s.fetch(r)
	if err != nil {
		writeErr(w, err)
		return
	}

	out := traceView{DecisionID: id}
	threshold := alertScore()
	out.Notes = append(out.Notes, fmt.Sprintf("高风险阈值 %.2f 仅用于显示，不改变处置", threshold))
	var score float64
	var hasScore bool
	for _, ev := range events {
		switch ev.Type {
		case judgedEventType:
			if ev.EventID == id {
				out.Judged = ev.Raw
			}
		case decisionEventType:
			var d topology.DecisionEvent
			if json.Unmarshal(ev.Raw, &d) == nil && d.DecisionID == id {
				out.Decision = ev.Raw
				score, hasScore = d.Score, true
				if d.Action == "ACTION_BLOCK" || (d.Severity != "" && d.Severity != "none") {
					out.Alerts.Real = true
					out.Alerts.Reason = "拦截 或 severity≠none（现行告警口径）"
				}
				if d.Severity == "" || d.Severity == "none" {
					out.Notes = append(out.Notes, "severity 档位未定（恒为 none）⇒ 影子模式下真实告警恒为 0")
				}
			}
		case analysisEventType:
			var a topology.AnalysisEvent
			if json.Unmarshal(ev.Raw, &a) != nil {
				continue
			}
			for _, evidence := range a.EvidenceIDs {
				if evidence == id {
					out.Analysis = append(out.Analysis, ev.Raw)
					break
				}
			}
		}
	}
	if hasScore && score >= threshold {
		out.Alerts.HighRisk = true
	}
	if out.Judged == nil {
		out.Notes = append(out.Notes, "没有适配器执行记录：可能是白名单/缓存之前的老数据，或事件已被缓冲挤出")
	}
	if out.Decision == nil {
		out.Notes = append(out.Notes, "没有核心判定记录：这条未被判定（白名单命中 / 判定缓存命中 / 判定失败后放行）")
	}
	writeJSON(w, out)
}

// alertScore 读取「高风险」显示阈值（仅显示用；默认 0.9，非法值回落到默认）。
func alertScore() float64 {
	raw := strings.TrimSpace(os.Getenv("SHEN_CONSOLE_ALERT_SCORE"))
	if raw == "" {
		return defaultAlertScore
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 || value > 1 {
		return defaultAlertScore
	}
	return value
}

// handleSummary 返回概览：总数、按决策分布、告警条数、时间范围。
func (s *server) handleSummary(w http.ResponseWriter, r *http.Request) {
	events, err := s.fetch(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	sum := summary{Total: len(events), ByAction: map[string]int{}}
	for _, ev := range events {
		if ev.Type == analysisEventType {
			sum.L4Conclusions++
		}
		if ev.Flow == nil {
			continue
		}
		action := ev.Flow.Action
		if action == "" {
			action = "unknown"
		}
		sum.ByAction[action]++
		if strings.EqualFold(action, "block") || (ev.Flow.Severity != "" && !strings.EqualFold(ev.Flow.Severity, "none")) {
			sum.Alerts++
		}
		t := ev.Flow.At
		if t.IsZero() {
			continue
		}
		if sum.FirstSeen == nil || t.Before(*sum.FirstSeen) {
			first := t
			sum.FirstSeen = &first
		}
		if sum.LastSeen == nil || t.After(*sum.LastSeen) {
			last := t
			sum.LastSeen = &last
		}
	}
	writeJSON(w, sum)
}

// configView 是「配置」页的数据形状（snake_case，与项目其余载荷一致）。
//
// 为什么再包一层而不直接回 proto：① proto 的 JSON 映射是给 gRPC 用的（camelCase），
// 页面的键要与其余接口一致；② 这层转换把「哪些字段允许出观测面」**显式写下来**了
// （见 `docs/spec/console-api.md` §2.1）—— 想加字段就得先在这里表态。
type configView struct {
	Policy policyView `json:"policy"`
	AI     aiView     `json:"ai"`
}

type policyView struct {
	PolicyID       string `json:"policy_id"`
	Version        uint64 `json:"version"`
	Checksum       string `json:"checksum"`
	RuleCount      int32  `json:"rule_count"`
	WhitelistCount int32  `json:"whitelist_count"`
}

type aiView struct {
	Enabled           bool     `json:"enabled"`
	Kinds             []string `json:"kinds"`
	Model             string   `json:"model"`
	ManifestPath      string   `json:"manifest_path"`
	Variants          int32    `json:"variants"`
	RotateCooldown    string   `json:"rotate_cooldown"`
	ManifestLoaded    bool     `json:"manifest_loaded"`
	ManifestVersion   uint64   `json:"manifest_version"`
	ManifestResources int32    `json:"manifest_resources"`
	ManifestContents  int32    `json:"manifest_contents"`
}

// handleConfig 返回核心当前生效状态的只读快照（「配置」页的数据源）。
//
// 核心未装配快照读侧时**透传错误**（页面显示「核心未提供」）——
// **不**返回一份全零的「正常」配置：那会让人以为引擎真的按 version=0 在跑
// （与核心侧拒绝回全零快照同一理由，`AR-15` 的精神）。
func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	snap, err := s.client.GetCoreSnapshot(ctx, &telemetryv1.GetCoreSnapshotRequest{})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, configView{
		Policy: policyView{
			PolicyID:       snap.GetPolicyId(),
			Version:        snap.GetPolicyVersion(),
			Checksum:       snap.GetPolicyChecksum(),
			RuleCount:      snap.GetRuleCount(),
			WhitelistCount: snap.GetWhitelistCount(),
		},
		AI: aiView{
			Enabled:           snap.GetAiEnabled(),
			Kinds:             snap.GetAiKinds(),
			Model:             snap.GetAiModel(),
			ManifestPath:      snap.GetAiManifestPath(),
			Variants:          snap.GetAiContentVariants(),
			RotateCooldown:    snap.GetAiRotateCooldown(),
			ManifestLoaded:    snap.GetAiManifestLoaded(),
			ManifestVersion:   snap.GetAiManifestVersion(),
			ManifestResources: snap.GetAiManifestResources(),
			ManifestContents:  snap.GetAiManifestContents(),
		},
	})
}

// viewOf 把一条 proto 事件解成页面的视图形状。
//
// **只有这一处解码**：拉（`/api/events` · `/api/flow`）与推（`/api/stream`）两条路径共用它 ——
// 各写一份必然会漂（同一条事件在两处显示成不同字段）。
func viewOf(ev *telemetryv1.TelemetryEvent) eventView {
	view := eventView{
		EventID:   ev.GetEventId(),
		Type:      ev.GetEventType(),
		ActorID:   ev.GetActorId(),
		SessionID: ev.GetSessionId(),
		Raw:       json.RawMessage(ev.GetPayload()),
	}
	if ev.GetCreatedAt() != nil {
		view.CreatedAt = ev.GetCreatedAt().AsTime()
	}
	if ev.GetEventType() == decisionEventType {
		var flow flowRecord
		if err := json.Unmarshal(ev.GetPayload(), &flow); err == nil {
			view.Flow = &flow
		}
	}
	return view
}

// sseFrame 是推给浏览器的一帧（一个形状，三种 kind）：
//
//	event  —— 一条事件（`view` 与拉取接口同形，页面不需要另一套渲染）
//	status —— 流自身状态：**丢了多少**（核心无背压，丢包必须可见）
//	closed —— 流结束（核心挂了/主动断开），页面据此提示「重连中」
//
// 为什么不让页面自己解载荷：解码在 Go 侧（`viewOf`），浏览器只负责画 —— 与拉取路径一致。
type sseFrame struct {
	Kind     string     `json:"kind"`
	View     *eventView `json:"view,omitempty"`
	Dropped  uint64     `json:"dropped,omitempty"`
	Buffered uint32     `json:"buffered,omitempty"`
	Capacity uint32     `json:"capacity,omitempty"`
	Error    string     `json:"error,omitempty"`
}

// handleStream 把核心的事件流以 SSE 推给浏览器（观测面推送，`ADR-0027`）。
//
// 两个要点：
//
//	① 本进程的 `http.Server` **没有** `WriteTimeout` —— 有的话长连接会被定期掐断（改它之前先看这里）；
//	② `since` 由页面带回来：断线重连不静默丢数据（核心先补一段历史再转流）。
func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "console: 本服务器不支持流式响应", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// 前置反代（若启用）不得缓冲流：否则「实时」会在反代那一层变成批量。
	w.Header().Set("X-Accel-Buffering", "no")

	req := &telemetryv1.WatchEventsRequest{SubscriberId: "console"}
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if at, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			req.Since = timestamppb.New(at)
		}
	}
	stream, err := s.client.WatchEvents(r.Context(), req)
	if err != nil {
		writeSSE(w, flusher, sseFrame{Kind: "closed", Error: err.Error()})
		return
	}
	log.Printf("console: 观测面订阅已建立（since=%q）", r.URL.Query().Get("since"))
	for {
		msg, err := stream.Recv()
		if err != nil {
			writeSSE(w, flusher, sseFrame{Kind: "closed", Error: streamEndReason(err)})
			return
		}
		switch body := msg.GetBody().(type) {
		case *telemetryv1.WatchEvent_Event:
			view := viewOf(body.Event)
			writeSSE(w, flusher, sseFrame{Kind: "event", View: &view})
		case *telemetryv1.WatchEvent_Status:
			writeSSE(w, flusher, sseFrame{
				Kind: "status", Dropped: body.Status.GetDropped(),
				Buffered: body.Status.GetBufferSize(), Capacity: body.Status.GetCapacity(),
			})
		}
	}
}

// streamEndReason 把「流为什么结束」翻成一句人话（页面直接展示）。
func streamEndReason(err error) string {
	switch {
	case errors.Is(err, io.EOF):
		return "核心结束了事件流"
	case status.Code(err) == codes.Unimplemented:
		return "核心未装配事件推送（老版本核心？）"
	case status.Code(err) == codes.Unavailable:
		return "核心不可达"
	default:
		return err.Error()
	}
}

// writeSSE 写一帧并立即 flush（不 flush 就不是「实时」）。
func writeSSE(w http.ResponseWriter, f http.Flusher, frame sseFrame) {
	payload, err := json.Marshal(frame)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(payload)
	_, _ = w.Write([]byte("\n\n"))
	f.Flush()
}

// fetch 向核心要最近事件（事件类型取查询参数 `type`）。
func (s *server) fetch(r *http.Request) ([]eventView, error) {
	return s.fetchType(r, r.URL.Query().Get("type"))
}

// fetchType 同 fetch，但由调用方**指定**事件类型（页面上每个块各取各的）。
func (s *server) fetchType(r *http.Request, eventType string) ([]eventView, error) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.client.ListEvents(ctx, &telemetryv1.ListEventsRequest{
		Limit:     uint32(limit),
		EventType: eventType,
	})
	if err != nil {
		return nil, fmt.Errorf("读核心事件失败（核心没起或地址不对？）：%w", err)
	}

	out := make([]eventView, 0, len(resp.GetEvents()))
	for _, ev := range resp.GetEvents() {
		out = append(out, viewOf(ev))
	}
	// 新的在前：核心已按 newest first 返回，这里再排一次以防上游实现变化。
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("console: 写响应失败：%v", err)
	}
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
