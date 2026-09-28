package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

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
}

// HTTPCaller 调 OpenAI 兼容的 `POST {base}/chat/completions`
// （DeepSeek：https://api.deepseek.com；OpenAI：https://api.openai.com/v1；Ollama：http://127.0.0.1:11434/v1）。
type HTTPCaller struct {
	client *http.Client
}

// NewHTTPCaller 创建调用器。**不跟随重定向**：Authorization 头跟着 3xx 跳到别的域名就是一次密钥外泄。
func NewHTTPCaller(timeout time.Duration) *HTTPCaller {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &HTTPCaller{client: &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("模型接口返回重定向：出于密钥安全不跟随，请直接填写最终地址")
		},
	}}
}

// Chat 发起一次对话补全。
func (c *HTTPCaller) Chat(ctx context.Context, base, key, model string, msgs []Message, maxTokens int) (Reply, error) {
	body, err := json.Marshal(map[string]any{
		"model": model, "messages": msgs, "max_tokens": maxTokens, "temperature": 0.3, "stream": false,
	})
	if err != nil {
		return Reply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Reply{}, &CallError{Msg: "构造请求失败"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	started := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		return Reply{}, &CallError{Msg: scrub(transportReason(err), key)}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	latency := time.Since(started)
	if err != nil {
		return Reply{Latency: latency}, &CallError{Status: resp.StatusCode, Msg: "读取响应失败"}
	}
	if resp.StatusCode/100 != 2 {
		return Reply{Latency: latency}, &CallError{Status: resp.StatusCode, Msg: scrub(errorReason(raw, resp.StatusCode), key)}
	}
	var out struct {
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return Reply{Latency: latency}, &CallError{Status: resp.StatusCode, Msg: "响应不是 OpenAI 兼容格式（缺 choices）"}
	}
	reply := Reply{Content: out.Choices[0].Message.Content, Latency: latency, Truncated: out.Choices[0].FinishReason == "length"}
	if out.Usage != nil {
		reply.Usage = *out.Usage
		if reply.Usage.Total == 0 {
			reply.Usage.Total = reply.Usage.Prompt + reply.Usage.Completion
		}
	} else {
		reply.Usage = estimate(msgs, reply.Content)
	}
	return reply, nil
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

// errorReason 提取服务商错误信息（OpenAI 格式 {"error":{"message":...}}），并给常见状态码一句人话。
func errorReason(raw []byte, status int) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	msg := ""
	if json.Unmarshal(raw, &e) == nil {
		msg = e.Error.Message
		if msg == "" {
			msg = e.Message
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
	}
	hint := map[int]string{401: "API Key 无效或已吊销", 402: "账户余额不足", 403: "无权访问该模型",
		404: "接口地址或模型名不对", 429: "触发限流或额度用尽"}[status]
	if hint != "" {
		return hint + "（" + truncate(msg, maxErrorChars) + "）"
	}
	return truncate(msg, maxErrorChars)
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
