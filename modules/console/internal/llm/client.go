package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// 出站调用经 github.com/sashabaranov/go-openai（MIT，零第三方依赖；DeepSeek 官方文档的 Go 示例同款）。
// 协议细节（请求组装 / 响应解析 / /models 列表）交给库；**安全不变量留在本文件**：
//
//	① 不跟随重定向 —— Authorization 头跟着 3xx 跳到别的域名就是一次密钥外泄；
//	② 响应体积上限 —— 异常大的回包不能拖垮进程；
//	③ 错误回显 scrub —— 有些服务商会在错误里回显部分 key；
//	④ 网络错误的人话提示 —— 运维看得懂。
//
// Message 是一条对话消息（OpenAI 兼容格式）。
type Message struct {
	Role    string `json:"role"` // system / user / assistant
	Content string `json:"content"`
}

// Usage 是一次调用消耗的 token（以服务商返回为准；不返回时为 0 并标注 estimated）。
type Usage struct {
	Prompt     int  `json:"prompt_tokens"`
	Completion int  `json:"completion_tokens"`
	Total      int  `json:"total_tokens"`
	Estimated  bool `json:"estimated,omitempty"`
}

// Reply 是一次调用的结果。
type Reply struct {
	Content   string        `json:"content"`
	Usage     Usage         `json:"usage"`
	Latency   time.Duration `json:"-"`
	Truncated bool          `json:"truncated,omitempty"` // finish_reason=length：答复被 max_tokens 截断
}

// CallError 是调用失败：只含状态码与服务商给的简短原因，**绝不含密钥**。
type CallError struct {
	Status int
	Msg    string
}

func (e *CallError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("模型接口返回 %d：%s", e.Status, e.Msg)
	}
	return e.Msg
}

const (
	maxResponseBytes = 4 << 20
	maxErrorChars    = 300
)

// Caller 抽象出站调用：测试里换成假服务商，不碰网络。
type Caller interface {
	Chat(ctx context.Context, base, key, model string, msgs []Message, maxTokens int) (Reply, error)
	// ListModels 拉取接口支持的模型清单（GET {base}/models）——「按已填信息探测可用模型」用。
	ListModels(ctx context.Context, base, key string) ([]string, error)
}

// limitedTransport 把响应体包上 LimitReader：异常大的回包在传输层就被截住。
type limitedTransport struct{ rt http.RoundTripper }

type limitedBody struct {
	io.Reader
	io.Closer
}

func (t *limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.rt.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = limitedBody{Reader: io.LimitReader(resp.Body, maxResponseBytes), Closer: resp.Body}
	return resp, nil
}

// HTTPCaller 调 OpenAI 兼容接口（chat/completions 与 models 都在库内拼路径）。
// （DeepSeek：https://api.deepseek.com；OpenAI：https://api.openai.com/v1；Ollama：http://127.0.0.1:11434/v1）
type HTTPCaller struct {
	client *http.Client
}

// NewHTTPCaller 创建调用器。**不跟随重定向**：Authorization 头跟着 3xx 跳到别的域名就是一次密钥外泄。
func NewHTTPCaller(timeout time.Duration) *HTTPCaller {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	inner := http.DefaultTransport
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		clone := t.Clone()
		inner = clone
	}
	return &HTTPCaller{client: &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("模型接口返回重定向：出于密钥安全不跟随，请直接填写最终地址")
		},
		Transport: &limitedTransport{rt: inner},
	}}
}

// openaiClient 按提供方地址与密钥组装库客户端（每次调用现场组装：无共享状态，密钥不驻留）。
func openaiClient(c *http.Client, base, key string) *openai.Client {
	cfg := openai.DefaultConfig(key)
	cfg.BaseURL = base
	cfg.HTTPClient = c
	return openai.NewClientWithConfig(cfg)
}

// Chat 发起一次对话补全。
func (c *HTTPCaller) Chat(ctx context.Context, base, key, model string, msgs []Message, maxTokens int) (Reply, error) {
	req := openai.ChatCompletionRequest{
		Model: model, Temperature: 0.3, Stream: false,
		Messages: make([]openai.ChatCompletionMessage, len(msgs)),
	}
	// 用 max_tokens 而不是库标记的 MaxCompletionTokens：本管控台面向 OpenAI **兼容**端点
	// （DeepSeek / Ollama / vLLM …），max_completion_tokens 只有 OpenAI 自家新模型支持。
	//lint:ignore SA1019 兼容端点统一走 max_tokens（见上一行注释）
	req.MaxTokens = maxTokens
	for i, m := range msgs {
		req.Messages[i] = openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
	}
	started := time.Now()
	resp, err := openaiClient(c.client, base, key).CreateChatCompletion(ctx, req)
	latency := time.Since(started)
	if err != nil {
		return Reply{Latency: latency}, callError(err, key)
	}
	if len(resp.Choices) == 0 {
		return Reply{Latency: latency}, &CallError{Msg: "响应不是 OpenAI 兼容格式（缺 choices）"}
	}
	reply := Reply{Content: resp.Choices[0].Message.Content, Latency: latency, Truncated: resp.Choices[0].FinishReason == openai.FinishReasonLength}
	if resp.Usage.TotalTokens > 0 {
		reply.Usage = Usage{Prompt: resp.Usage.PromptTokens, Completion: resp.Usage.CompletionTokens, Total: resp.Usage.TotalTokens}
	} else if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		reply.Usage = Usage{Prompt: resp.Usage.PromptTokens, Completion: resp.Usage.CompletionTokens,
			Total: resp.Usage.PromptTokens + resp.Usage.CompletionTokens}
	} else {
		reply.Usage = estimate(msgs, reply.Content) // 服务商没回 usage：粗估并显式标注
	}
	return reply, nil
}

// ListModels 拉取模型清单（按名称排序，去重）。
func (c *HTTPCaller) ListModels(ctx context.Context, base, key string) ([]string, error) {
	page, err := openaiClient(c.client, base, key).ListModels(ctx)
	if err != nil {
		return nil, callError(err, key)
	}
	seen := make(map[string]bool, len(page.Models))
	out := make([]string, 0, len(page.Models))
	for _, m := range page.Models {
		if id := strings.TrimSpace(m.ID); id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// callError 把库错误翻译成 CallError：API 错误给状态码与人话提示；网络错误按成因归类；一律 scrub。
func callError(err error, key string) error {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		msg := strings.TrimSpace(apiErr.Message)
		hint := map[int]string{401: "API Key 无效或已吊销", 402: "账户余额不足", 403: "无权访问该模型",
			404: "接口地址或模型名不对", 429: "触发限流或额度用尽"}[apiErr.HTTPStatusCode]
		if hint != "" {
			msg = hint + "（" + truncate(msg, maxErrorChars) + "）"
		}
		return &CallError{Status: apiErr.HTTPStatusCode, Msg: scrub(truncate(msg, maxErrorChars+40), key)}
	}
	return &CallError{Msg: scrub(transportReason(err), key)}
}

// estimate 在服务商不回 usage 时粗估（中文约 1 字 ≈ 1 token、英文约 4 字符 ≈ 1 token），并显式标注。
func estimate(msgs []Message, answer string) Usage {
	count := func(s string) int {
		n := 0
		for _, r := range s {
			if r > 0x2E80 {
				n += 4
			} else {
				n++
			}
		}
		return n/4 + 1
	}
	p := 0
	for _, m := range msgs {
		p += count(m.Content) + 4
	}
	c := count(answer)
	return Usage{Prompt: p, Completion: c, Total: p + c, Estimated: true}
}

func transportReason(err error) string {
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(msg, "Client.Timeout"):
		return "请求超时（模型响应过慢或网络不可达）"
	case errors.Is(err, context.Canceled):
		return "请求已取消"
	case strings.Contains(msg, "no such host"):
		return "域名解析失败（检查接口地址或容器的出站网络）"
	case strings.Contains(msg, "connection refused"):
		return "连接被拒绝（服务未启动或端口不对）"
	case strings.Contains(msg, "certificate"):
		return "TLS 证书校验失败"
	case strings.Contains(msg, "重定向"):
		return "模型接口返回重定向：出于密钥安全不跟随，请直接填写最终地址"
	}
	return "网络错误：" + truncate(msg, 160)
}

// scrub 从任何要回显的文本里抹掉密钥（有些服务商会在错误里回显部分 key）。
func scrub(s, key string) string {
	if key != "" {
		s = strings.ReplaceAll(s, key, Hint(key))
		if len(key) > 12 {
			s = strings.ReplaceAll(s, key[:len(key)-4], "****")
		}
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
