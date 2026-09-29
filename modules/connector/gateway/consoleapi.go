package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ConsoleClient 是控制台集成面的薄客户端：key 表拉取、自动登记上报、会话上报。
// 出站只有这三个动作 —— 控制台自身不出站的边界不因本客户端而改变。
type ConsoleClient struct {
	base  string // 如 http://127.0.0.1:9445
	token string
	hc    *http.Client
}

// NewConsoleClient 创建客户端（超时 10s：集成面在同机回环，超过它说明 console 挂了）。
func NewConsoleClient(base, token string) *ConsoleClient {
	return &ConsoleClient{base: strings.TrimRight(base, "/"), token: token,
		hc: &http.Client{Timeout: 10 * time.Second}}
}

func (c *ConsoleClient) post(ctx context.Context, path string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("console %s：%w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("console %s 返回 %d", path, resp.StatusCode)
	}
	return nil
}

// PullKeys 拉取凭证哈希表。
func (c *ConsoleClient) PullKeys(ctx context.Context) ([]ConsoleKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/integration/keys", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("console 拉取 key 表失败：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("console 拉取 key 表返回 %d", resp.StatusCode)
	}
	var out struct {
		Keys []struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Hosts   []string `json:"hosts"`
			KeyHash string   `json:"key_hash"`
			Revoked bool     `json:"revoked"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("console key 表解码失败：%w", err)
	}
	keys := make([]ConsoleKey, 0, len(out.Keys))
	for _, k := range out.Keys {
		keys = append(keys, ConsoleKey{ID: k.ID, Name: k.Name, Hosts: k.Hosts, KeyHash: k.KeyHash, Revoked: k.Revoked})
	}
	return keys, nil
}

// ReportRegister 上报自动登记（连接器握手成功时）。
func (c *ConsoleClient) ReportRegister(ctx context.Context, name string, hosts []string, localAddr string) error {
	return c.post(ctx, "/api/v1/integration/register", map[string]any{
		"name": name, "hosts": hosts, "local_addr": localAddr})
}

// SessionReport 是会话上报的载荷（与控制台 connector.SessionState 对应）。
type SessionReport struct {
	SessionID        string   `json:"session_id"`
	CredentialID     string   `json:"credential_id"`
	Name             string   `json:"name"`
	Hosts            []string `json:"hosts"`
	LocalAddr        string   `json:"local_addr"`
	ConnectorIP      string   `json:"connector_ip"`
	ConnectorVersion string   `json:"connector_version"`
	GatewayNode      string   `json:"gateway_node"`
	Online           bool     `json:"online"`
	PlannedClose     bool     `json:"planned_close"`
	RttMs            int64    `json:"rtt_ms"`
}

// ReportSession 上报会话状态。
func (c *ConsoleClient) ReportSession(ctx context.Context, rep SessionReport) error {
	return c.post(ctx, "/api/v1/integration/sessions", rep)
}
