package proxy

// SHEN-M3-01：把「业务侧响应零改写」从注释约束升级为**可运行的证明**。
//
// 做法：同一请求分别**直连源站**与**经代理**（核心不可达 ⇒ 一律 route_origin），
// 断言状态码、头（排除少量由中介层合法添加的项）与实体**逐字节一致**。
//
// 为什么必须「直连 vs 经代理」成对跑：只测经代理的一侧，无法区分「代理没改」
// 与「源站本来就长这样」。成对比较才是 INT-8 的判据。
//
// 覆盖四类：GET · POST（写方法）· 压缩响应 · 大响应。

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// hopOrGuardHeaders 是**允许不一致**的响应头：由中介层/传输层合法添加或改写。
//
//   - Date / Server / Via / Alt-Svc：Caddy 与管理层会单独写；
//   - Content-Length：分块传输时由下层的 chunked 表达，实体仍须逐字节一致。
var hopOrGuardHeaders = map[string]bool{
	"Date": true, "Server": true, "Via": true, "Alt-Svc": true, "Content-Length": true,
}

func headerDiff(got, want http.Header) []string {
	var diffs []string
	for k := range got {
		if hopOrGuardHeaders[k] {
			continue
		}
		if g, w := strings.Join(got[k], ","), strings.Join(want[k], ","); g != w {
			diffs = append(diffs, fmt.Sprintf("%s: 代理=%q 源站=%q", k, g, w))
		}
	}
	for k := range want {
		if hopOrGuardHeaders[k] {
			continue
		}
		if _, ok := got[k]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s: 代理缺失（源站=%q）", k, strings.Join(want[k], ",")))
		}
	}
	return diffs
}

// TestOriginResponseIsByteIdentical 逐类断言：经代理与直连源站完全一致。
func TestOriginResponseIsByteIdentical(t *testing.T) {
	var hits, writes atomic.Int64

	big := bytes.Repeat([]byte("0123456789abcdef"), 128*1024) // 2 MiB，跨过注入缓冲上限
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(`{"status":"ok","rows":[1,2,3]}`))
	_ = zw.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("X-Origin-Marker", "shen-origin")
		switch r.URL.Path {
		case "/big":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(big)
		case "/gzip":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("X-App-Version", "v42")
			_, _ = w.Write(gz.Bytes())
		case "/echo":
			if r.Method == http.MethodPost {
				writes.Add(1)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			body, _ := io.ReadAll(r.Body)
			_, _ = w.Write([]byte("<html><body>echo:" + string(body) + "</body></html>"))
		case "/err":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("origin failed"))
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Page", "home")
			_, _ = w.Write([]byte("<html><body>home</body></html>"))
		}
	}))
	defer origin.Close()

	proxyAddr := startForwardingProxy(t, forwardingHandler(origin.URL))
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}

	do := func(base, method, path, body string) (*http.Response, []byte) {
		t.Helper()
		var rdr io.Reader
		if body != "" {
			rdr = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, base+path, rdr)
		if err != nil {
			t.Fatalf("构造请求失败：%v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s 失败：%v", method, path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		return resp, raw
	}

	cases := []struct{ name, method, path, body string }{
		{"GET HTML", http.MethodGet, "/", ""},
		{"POST 写请求", http.MethodPost, "/echo", "amount=1&id=42"},
		{"压缩响应", http.MethodGet, "/gzip", ""},
		{"大响应 2MiB", http.MethodGet, "/big", ""},
		{"源站 5xx", http.MethodGet, "/err", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 基线：先直连源站一次，记下写次数，再经代理一次 —— 代理这一次必须**恰好**多一次写，
			// 不能因为判定/重试/重复转发让源站被写两遍（那才是真正的业务事故）。
			writesBefore := writes.Load()
			wantResp, wantBody := do(origin.URL, c.method, c.path, c.body)
			writesAfterDirect := writes.Load()

			gotResp, gotBody := do("http://"+proxyAddr, c.method, c.path, c.body)
			writesAfterProxy := writes.Load()

			if gotResp.StatusCode != wantResp.StatusCode {
				t.Fatalf("状态码不一致：代理 %d，源站 %d", gotResp.StatusCode, wantResp.StatusCode)
			}
			if diffs := headerDiff(gotResp.Header, wantResp.Header); len(diffs) > 0 {
				t.Fatalf("响应头不一致（INT-8：业务侧响应不得改写）：\n  %s", strings.Join(diffs, "\n  "))
			}
			if !bytes.Equal(gotBody, wantBody) {
				t.Fatalf("实体不一致（INT-8）：代理 %d 字节，源站 %d 字节；首差 %s",
					len(gotBody), len(wantBody), firstDiff(gotBody, wantBody))
			}

			// 副作用断言：写方法经代理只让源站多处理**一次**。
			if c.method == http.MethodPost {
				if d := writesAfterDirect - writesBefore; d != 1 {
					t.Fatalf("基线的写次数应为 1，实际 %d", d)
				}
				if d := writesAfterProxy - writesAfterDirect; d != 1 {
					t.Fatalf("经代理的写次数应为 1，实际 %d（重复写 = 业务被改两次）", d)
				}
			}
		})
	}

	// 每个用例两次请求（基线 + 代理），源站命中次数必须恰好等于 2×用例数：
	// 代理不得自行短路、也不得重放请求。
	if got, want := hits.Load(), int64(2*len(cases)); got != want {
		t.Fatalf("源站命中次数应为 %d，实际 %d", want, got)
	}
}

// firstDiff 返回前 40 字节内第一处差异的说明（大响应失败时不必把 2 MiB 全打出来）。
func firstDiff(a, b []byte) string {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			lo := max(0, i-8)
			return fmt.Sprintf("偏移 %d：代理 %q / 源站 %q", i, a[lo:min(len(a), i+16)], b[lo:min(len(b), i+16)])
		}
	}
	return "长度不同但前缀一致"
}
