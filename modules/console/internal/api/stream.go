package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "shen/common/api/telemetry/v1"
)

// sseFrame 是推给浏览器的一帧：
//
//	event  —— 一条事件（view 与拉取接口同形；request_judged 额外带合并好的 row，页面直接追加）
//	status —— 流自身状态：核心侧丢了多少（核心无背压，丢包必须可见）
//	closed —— 流结束（核心断开 / 会话失效），页面据此提示并重连
type sseFrame struct {
	Kind     string      `json:"kind"`
	View     *eventView  `json:"view,omitempty"`
	Row      *TrafficRow `json:"row,omitempty"`
	Dropped  uint64      `json:"dropped,omitempty"`
	Buffered uint32      `json:"buffered,omitempty"`
	Capacity uint32      `json:"capacity,omitempty"`
	Error    string      `json:"error,omitempty"`
	Code     string      `json:"code,omitempty"`
}

// handleStream 把核心事件流以 SSE 推给浏览器。
//
// 要点：
//   - 进程的 http.Server **不设** WriteTimeout（否则长连接被定期掐断）；nginx 侧关闭缓冲；
//   - `since` 由页面带回：断线重连不丢数据（核心先补一段历史再转流）；
//   - 定期心跳注释行：穿过中间层的空闲超时，同时借机复核会话（被注销 / 改角色后流立即结束）；
//   - 并发连接数有上限：SSE 是长连接，无上限等于把核心读面暴露给资源耗尽。
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "服务器不支持流式响应")
		return
	}
	if n := s.streams.Add(1); int(n) > s.cfg.MaxStreams {
		s.streams.Add(-1)
		writeError(w, http.StatusServiceUnavailable, "too_many_streams", "实时连接数已达上限，请关闭多余页面")
		return
	}
	defer s.streams.Add(-1)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-store")
	h.Set("X-Accel-Buffering", "no")

	p := principalOf(r)
	req := &telemetryv1.WatchEventsRequest{SubscriberId: "console:" + p.Username, CatchUpLimit: 500}
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if at, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			req.Since = timestamppb.New(at)
		}
	}
	stream, err := s.core.WatchEvents(r.Context(), req)
	if err != nil {
		// 非 200：浏览器 EventSource 走 onerror（而不是 onopen）—— 否则前端会把「核心不可达」误显示成「实时」。
		h.Set("Content-Type", "application/json; charset=utf-8")
		writeError(w, http.StatusServiceUnavailable, "core_unavailable", streamEndReason(err))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "retry: 3000\n\n")
	flusher.Flush()

	msgs := make(chan *telemetryv1.WatchEvent, 64)
	errc := make(chan error, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				errc <- err
				return
			}
			select {
			case msgs <- msg:
			case <-r.Context().Done():
				return
			}
		}
	}()

	ticker := time.NewTicker(s.cfg.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case err := <-errc:
			writeSSE(w, flusher, sseFrame{Kind: "closed", Error: streamEndReason(err), Code: "core_closed"})
			return
		case <-ticker.C:
			if !p.Token && !s.stillValid(r) {
				writeSSE(w, flusher, sseFrame{Kind: "closed", Error: "会话已失效，请重新登录", Code: "unauthenticated"})
				return
			}
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case msg := <-msgs:
			s.forward(w, flusher, msg)
		}
	}
}

func (s *Server) forward(w http.ResponseWriter, f http.Flusher, msg *telemetryv1.WatchEvent) {
	switch body := msg.GetBody().(type) {
	case *telemetryv1.WatchEvent_Event:
		view := viewOf(body.Event)
		frame := sseFrame{Kind: "event", View: &view}
		if view.Judged != nil {
			row := s.rowFromJudged(view.CreatedAt, view.Judged)
			s.enrich(&row)
			frame.Row = &row
		}
		if view.Flow != nil || view.Judged != nil {
			view.Raw = nil // 载荷已解成对象：不重复带原文（其余类型保留原文供页面展示）
		}
		writeSSE(w, f, frame)
	case *telemetryv1.WatchEvent_Status:
		writeSSE(w, f, sseFrame{Kind: "status", Dropped: body.Status.GetDropped(),
			Buffered: body.Status.GetBufferSize(), Capacity: body.Status.GetCapacity()})
	}
}

func (s *Server) stillValid(r *http.Request) bool {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return false
	}
	_, ok := s.auth.Authenticate(c.Value)
	return ok
}

func streamEndReason(err error) string {
	switch {
	case errors.Is(err, io.EOF):
		return "核心结束了事件流"
	case status.Code(err) == codes.Unimplemented:
		return "核心未装配事件推送（老版本核心？）"
	case status.Code(err) == codes.Unavailable:
		return "核心不可达"
	case status.Code(err) == codes.Canceled:
		return "连接已取消"
	default:
		return err.Error()
	}
}

func writeSSE(w http.ResponseWriter, f http.Flusher, frame sseFrame) {
	payload, err := json.Marshal(frame)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, "data: ")
	_, _ = w.Write(payload)
	_, _ = io.WriteString(w, "\n\n")
	f.Flush()
}
