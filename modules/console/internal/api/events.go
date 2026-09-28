package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "shen/common/api/telemetry/v1"
)

// 观测面事件类型（跨进程契约）。
const (
	decisionEventType = "decision"       // 核心判定（意图：分值 / 信号 / 三值）
	analysisEventType = "analysis"       // L4 近线分析结论
	judgedEventType   = "request_judged" // 适配器逐请求执行（实际落点 / 投递结果 / host / source_ip）
)

// 核心内存实现只保留最近 4096 条事件（store.DefaultEventBuffer）：单次最多取这么多。
const maxListLimit = 4096

// flowRecord 是判定事件的载荷形状（与核心 DecisionRecord 同形，snake_case）。
type flowRecord struct {
	DecisionID string    `json:"decision_id"`
	SourceIP   string    `json:"source_ip"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Query      string    `json:"query,omitempty"`
	UserAgent  string    `json:"user_agent"`
	SessionID  string    `json:"session_id,omitempty"`
	Action     string    `json:"action"`
	Severity   string    `json:"severity"`
	Backend    string    `json:"backend"`
	Score      float64   `json:"score"`
	Signals    []string  `json:"signals"`
	At         time.Time `json:"at"`
}

// judgedRecord 是 request_judged 的载荷形状（与适配器 judgedEventPayload 同形）。
type judgedRecord struct {
	DecisionID     string  `json:"decision_id"`
	Method         string  `json:"method"`
	Path           string  `json:"path"`
	UA             string  `json:"ua"`
	Action         string  `json:"action"`
	Shadow         bool    `json:"shadow"`
	DecisionError  string  `json:"decision_error"`
	Executed       string  `json:"executed"`
	Dispatched     string  `json:"dispatched"` // 仅 executed=cache：缓存决策实际走到的落点
	Backend        string  `json:"backend"`
	Status         int     `json:"status"`
	Bytes          int     `json:"bytes"`
	DurationMs     float64 `json:"duration_ms"`
	DeliveryResult string  `json:"delivery_result"`
	Inject         string  `json:"inject"`
	ContentID      string  `json:"content_id"`
	Host           string  `json:"host"`
	SourceIP       string  `json:"source_ip"`
}

// eventView 是页面消费的事件视图（载荷已解成对象）。
type eventView struct {
	EventID   string          `json:"event_id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	ActorID   string          `json:"actor_id"`
	SessionID string          `json:"session_id"`
	Flow      *flowRecord     `json:"flow,omitempty"`
	Judged    *judgedRecord   `json:"judged,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

// viewOf 是**唯一**的事件解码点：拉取（REST）与推送（SSE）共用，避免两处各解一份而漂移。
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
	switch ev.GetEventType() {
	case decisionEventType:
		var flow flowRecord
		if json.Unmarshal(ev.GetPayload(), &flow) == nil {
			if flow.At.IsZero() {
				flow.At = view.CreatedAt
			}
			view.Flow = &flow
		}
	case judgedEventType:
		var j judgedRecord
		if json.Unmarshal(ev.GetPayload(), &j) == nil {
			if j.DecisionID == "" {
				j.DecisionID = ev.GetEventId() // 旧数据兼容：事件 id 曾等于 decision_id
			}
			view.Judged = &j
		}
	}
	return view
}

// listEvents 向核心取最近事件（newest first）。since 为零表示不限时间。
func (s *Server) listEvents(ctx context.Context, eventType string, limit int, since time.Time) ([]eventView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req := &telemetryv1.ListEventsRequest{Limit: uint32(limit), EventType: eventType}
	if !since.IsZero() {
		req.Since = timestamppb.New(since)
	}
	resp, err := s.core.ListEvents(ctx, req)
	if err != nil {
		return nil, &coreError{err: fmt.Errorf("读核心事件失败（核心没起或地址不对？）：%w", err)}
	}
	out := make([]eventView, 0, len(resp.GetEvents()))
	for _, ev := range resp.GetEvents() {
		out = append(out, viewOf(ev))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// coreError 标记「核心不可用」类错误（映射为 502，页面据此提示检查核心）。
type coreError struct{ err error }

func (e *coreError) Error() string { return e.err.Error() }
func (e *coreError) Unwrap() error { return e.err }

// intParam 读取整数查询参数；缺失或越界时回落默认值。
func intParam(r *http.Request, key string, def, lo, hi int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		return def
	}
	return n
}

// normAction 把两种编码（核心 `route_*` 与适配器 `ACTION_*`）统一成三值之一。
func normAction(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "route_origin", "action_origin", "origin":
		return "route_origin"
	case "route_mirage", "action_mirage", "mirage":
		return "route_mirage"
	case "block", "action_block":
		return "block"
	case "", "action_unspecified":
		return ""
	default:
		return "unknown"
	}
}
