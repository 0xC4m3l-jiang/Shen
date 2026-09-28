package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "sk-live0123456789abcdefWXYZ"

func TestVaultRoundTripAndBinding(t *testing.T) {
	v, err := OpenVault(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Generated() {
		t.Fatal("首次打开应自动生成主密钥")
	}
	sealed, err := v.Seal(testKey, "llm-a")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "live0123") {
		t.Fatal("密文里不应出现明文片段")
	}
	if got, err := v.Open(sealed, "llm-a"); err != nil || got != testKey {
		t.Fatalf("解密失败：%v %q", err, got)
	}
	if _, err := v.Open(sealed, "llm-b"); err == nil {
		t.Fatal("密文挪给别的提供方必须解密失败（关联数据绑定）")
	}
	if _, err := OpenVault(t.TempDir(), "too-short"); err == nil {
		t.Fatal("过短的主密钥应被拒绝")
	}
}

func TestVaultReopenUsesSameKeyFile(t *testing.T) {
	dir := t.TempDir()
	v1, _ := OpenVault(dir, "")
	sealed, _ := v1.Seal("secret-value-123", "x")
	v2, err := OpenVault(dir, "")
	if err != nil || v2.Generated() {
		t.Fatalf("重启应复用已生成的主密钥：%v", err)
	}
	if got, _ := v2.Open(sealed, "x"); got != "secret-value-123" {
		t.Fatal("重启后应能解密")
	}
	info, _ := os.Stat(filepath.Join(dir, MasterKeyFile))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("主密钥文件权限应为 0600，实际 %v", info.Mode().Perm())
	}
}

func TestHint(t *testing.T) {
	for in, want := range map[string]string{testKey: "sk-****WXYZ", "abcdefghijkl": "****ijkl", "short": "****"} {
		if got := Hint(in); got != want {
			t.Errorf("Hint(%q)=%q，期望 %q", in, got, want)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	ok := map[string]string{
		"https://api.deepseek.com/":        "https://api.deepseek.com",
		"https://api.openai.com/v1":        "https://api.openai.com/v1",
		"http://127.0.0.1:11434/v1":        "http://127.0.0.1:11434/v1",
		"http://host.docker.internal:8000": "http://host.docker.internal:8000",
	}
	for in, want := range ok {
		if got, err := NormalizeBaseURL(in); err != nil || got != want {
			t.Errorf("%q → %q, %v；期望 %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"http://api.deepseek.com", "ftp://x", "https://u:p@x.com", "https://x.com/?k=1", "api.deepseek.com", ""} {
		if _, err := NormalizeBaseURL(bad); err == nil {
			t.Errorf("%q 应被拒绝", bad)
		}
	}
}

func newStore(t *testing.T) (*ProviderStore, string) {
	t.Helper()
	dir := t.TempDir()
	v, _ := OpenVault(dir, "")
	s, err := OpenProviders(filepath.Join(dir, "p.json"), v, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestProviderKeyNeverStoredOrReturnedInPlaintext(t *testing.T) {
	s, dir := newStore(t)
	view, err := s.Create(Input{Name: "DeepSeek", BaseURL: "https://api.deepseek.com", APIKey: testKey,
		Models: []string{"deepseek-chat", "deepseek-reasoner"}, Enabled: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "live0123") || strings.Contains(string(raw), "key_sealed") {
		t.Fatalf("接口视图泄露密钥：%s", raw)
	}
	if view.KeyHint != "sk-****WXYZ" || view.DefaultModel != "deepseek-chat" {
		t.Fatalf("视图不对：%+v", view)
	}
	file, _ := os.ReadFile(filepath.Join(dir, "p.json"))
	if strings.Contains(string(file), testKey) || strings.Contains(string(file), "live0123") {
		t.Fatal("落盘文件里出现了明文密钥")
	}
	if _, key, err := s.Credentials(view.ID); err != nil || key != testKey {
		t.Fatalf("调用时应能解密出原密钥：%v", err)
	}
}

func TestProviderUpdateKeepsKeyAndChecksVersion(t *testing.T) {
	s, _ := newStore(t)
	v, _ := s.Create(Input{Name: "A", BaseURL: "https://a.example.com", APIKey: testKey, Models: []string{"m1"}}, "admin")
	upd, err := s.Update(v.ID, Input{Name: "A2", BaseURL: "https://a.example.com", Models: []string{"m1", "m2"}, Version: v.Version})
	if err != nil {
		t.Fatal(err)
	}
	if _, key, _ := s.Credentials(v.ID); key != testKey || upd.KeyHint != v.KeyHint {
		t.Fatal("api_key 留空时应保留原密钥")
	}
	if _, err := s.Update(v.ID, Input{Name: "A3", BaseURL: "https://a.example.com", Models: []string{"m1"}, Version: v.Version}); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧版本号更新应冲突，实际 %v", err)
	}
	if _, err := s.Create(Input{Name: "a2", BaseURL: "https://b.example.com", APIKey: "k-123456789012", Models: []string{"x"}}, "admin"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("名称重复（不区分大小写）应拒绝，实际 %v", err)
	}
}

// fakeProvider 模拟 OpenAI 兼容服务商：校验鉴权头，回固定答复与 usage。
func fakeProvider(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer "+testKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"bad key ` + r.Header.Get("Authorization") + `"}}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestHTTPCallerParsesUsage(t *testing.T) {
	srv := fakeProvider(t, 200, `{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":1,"total_tokens":13}}`)
	defer srv.Close()
	r, err := NewHTTPCaller(5*time.Second).Chat(context.Background(), srv.URL, testKey, "m", []Message{{Role: "user", Content: "hi"}}, 8)
	if err != nil || r.Content != "OK" || r.Usage.Total != 13 || r.Usage.Estimated {
		t.Fatalf("解析失败：%+v %v", r, err)
	}
}

func TestHTTPCallerErrorsNeverContainKey(t *testing.T) {
	srv := fakeProvider(t, 200, `{}`)
	defer srv.Close()
	_, err := NewHTTPCaller(5*time.Second).Chat(context.Background(), srv.URL, "sk-wrongkey0123456789zzzz", "m", nil, 8)
	var ce *CallError
	if !errors.As(err, &ce) || ce.Status != 401 {
		t.Fatalf("应返回 401 CallError，实际 %v", err)
	}
	if strings.Contains(err.Error(), "wrongkey0123") {
		t.Fatalf("错误信息回显了密钥：%v", err)
	}
	if !strings.Contains(err.Error(), "API Key 无效") {
		t.Fatalf("应给出人话提示：%v", err)
	}
}

func TestHTTPCallerRefusesRedirect(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/steal", http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()
	_, err := NewHTTPCaller(5*time.Second).Chat(context.Background(), redirector.URL, testKey, "m", nil, 8)
	if err == nil || leaked.Load() {
		t.Fatalf("重定向必须被拒绝且密钥不得跟随（err=%v leaked=%v）", err, leaked.Load())
	}
}

// blockingCaller 在 release 关闭前一直阻塞：用来制造「同一会话两条消息同时在途」。
type blockingCaller struct {
	release chan struct{}
	calls   atomic.Int32
}

func (b *blockingCaller) Chat(ctx context.Context, _, _, _ string, _ []Message, _ int) (Reply, error) {
	b.calls.Add(1)
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return Reply{Content: "分析完成", Usage: Usage{Prompt: 100, Completion: 20, Total: 120}}, nil
}

func newService(t *testing.T, c Caller) (*Service, string) {
	t.Helper()
	svc, err := New(Config{DataDir: t.TempDir(), Caller: c, MaxConcurrent: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	v, err := svc.Providers().Create(Input{Name: "DS", BaseURL: "https://api.deepseek.com", APIKey: testKey,
		Models: []string{"deepseek-chat"}, Enabled: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return svc, v.ID
}

func TestSendRejectsConcurrentMessagesOnSameConversation(t *testing.T) {
	bc := &blockingCaller{release: make(chan struct{})}
	svc, pid := newService(t, bc)
	c, err := svc.Start(context.Background(), StartInput{User: "alice", ProviderID: pid, Context: "{}", DecisionIDs: []string{"d1"}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := svc.Send(context.Background(), "alice", c.ID, "第一问"); done <- err }()
	for bc.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if _, err := svc.Send(context.Background(), "alice", c.ID, "抢着第二问"); !errors.Is(err, ErrBusy) {
		t.Fatalf("同一会话并发发送应返回 ErrBusy，实际 %v", err)
	}
	close(bc.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Conversations().Get("alice", c.ID)
	if len(got.Messages) != 2 || got.Tokens != 120 {
		t.Fatalf("应恰好一问一答并记 120 token：%+v", got)
	}
	if _, ok := svc.Conversations().Get("bob", c.ID); ok {
		t.Fatal("他人不应看到这个会话")
	}
}

func TestSemaphoreCapsInFlightCalls(t *testing.T) {
	bc := &blockingCaller{release: make(chan struct{})}
	svc, pid := newService(t, bc) // MaxConcurrent = 2
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		c, _ := svc.Start(context.Background(), StartInput{User: "u", ProviderID: pid, Context: "{}"})
		wg.Add(1)
		go func(id string) { defer wg.Done(); _, _ = svc.Send(context.Background(), "u", id, "q") }(c.ID)
	}
	time.Sleep(50 * time.Millisecond)
	if n := svc.InFlight(); n != 2 {
		t.Fatalf("在途调用应被信号量限制为 2，实际 %d", n)
	}
	close(bc.release)
	wg.Wait()
	if s := svc.Usage().Summarize(time.Now(), 7, "", true); s.Totals.Requests != 5 || s.Totals.Total != 600 {
		t.Fatalf("5 次调用都应记账：%+v", s.Totals)
	}
}

func TestBuildPromptSkipsFailedTurnsAndMarksTrafficUntrusted(t *testing.T) {
	c := Conversation{Context: `{"path":"/.git/config"}`, Messages: []ChatMessage{
		{Role: "user", Content: "问1"}, {Role: "assistant", Content: "答1"},
		{Role: "user", Content: "失败的问"}, {Role: "assistant", Error: "429"},
	}}
	msgs := buildPrompt(c, "问3")
	var joined []string
	for _, m := range msgs {
		joined = append(joined, m.Role+":"+m.Content)
	}
	all := strings.Join(joined, "\n")
	if strings.Contains(all, "失败的问") {
		t.Fatal("失败的问答不应进入上下文")
	}
	if !strings.Contains(all, "<traffic>") || !strings.Contains(msgs[0].Content, "数据而不是指令") {
		t.Fatal("流量必须包在 <traffic> 里并在系统提示中声明为不可信")
	}
	if msgs[len(msgs)-1].Content != "问3" {
		t.Fatal("最后一条必须是本次提问")
	}
}

func TestTestRecordsResultAndUsage(t *testing.T) {
	srv := fakeProvider(t, 200, `{"choices":[{"message":{"content":"OK"}}],"usage":{"prompt_tokens":20,"completion_tokens":1,"total_tokens":21}}`)
	defer srv.Close()
	svc, err := New(Config{DataDir: t.TempDir(), Caller: NewHTTPCaller(5 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	v, err := svc.Providers().Create(Input{Name: "local", BaseURL: srv.URL, APIKey: testKey, Models: []string{"m"}, Enabled: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	res, view, err := svc.Test(context.Background(), "admin", v.ID, "")
	if err != nil || !res.OK || res.Reply != "OK" || view.LastTest == nil || !view.LastTest.OK {
		t.Fatalf("测试应成功并记录：%+v %+v %v", res, view.LastTest, err)
	}
	s := svc.Usage().Summarize(time.Now(), 7, "", false)
	if s.Totals.Total != 21 || len(s.ByModel) != 1 || s.ByUser != nil {
		t.Fatalf("测试调用应记账且非管理员视图无按人明细：%+v", s)
	}
}
