// Package main 是 `fingerprint` —— TLS 指纹采集与对比工具。
//
// 它服务于实验 `E2`（见 docs/background/notes/pending-experiments.md）：
// **对同一站点采集两组 TLS 特征，看我们的栈能不能与真实站对齐**。
//
// 为什么这件事是 P0：威胁模型的 `A2`（对手无法察觉自己被骗）是本项目的唯一命题；
// 而 TLS 握手是攻击者**最先**看到的东西 —— 一旦它一眼可辨，后面的伪装都不必谈。
//
// 采什么（服务端侧 = 攻击者视角）：
//   - 协商版本 · 选用密码套件 · ALPN · 会话恢复支持
//   - **ServerHello 的扩展类型与顺序**（JA3S 的输入）
//   - 证书链摘要（签发者 / 密钥类型 / 签名算法 / SAN 数量 / 有效期）
//   - 我们**自己**发出的 ClientHello 指纹（JA3）—— 用于上游方向（Caddy→业务/幻境后端）
//
// 用法：
//
//	fingerprint -mode capture -addr example.com:443 -out real.json
//	fingerprint -mode capture -addr 127.0.0.1:8443 -sni shop.example.com -out ours.json
//	fingerprint -mode diff -a real.json -b ours.json
//
// 判定口径与后果（`E2` 的判定标准，不可事后调整）：
//
//	不可区分            → ✅ 风险 R-1 关闭
//	可区分但属可配置项  → ⚠️ 调我方栈配置使其对齐，重跑
//	可区分且无法对齐    → ❌ 威胁模型 R-1 必须重估整个欺骗命题
package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"time"
)

// Capture 是一次握手的采集结果（落盘为 JSON，供 diff 与留存原始数据）。
type Capture struct {
	// 元信息：让结果可复现（同一条命令能在别的机器上重跑）。
	Target     string    `json:"target"`
	SNI        string    `json:"sni"`
	CapturedAt time.Time `json:"captured_at"`
	ToolNote   string    `json:"tool_note"`

	// 协商结果（攻击者一眼可见的部分）。
	TLSVersion      string `json:"tls_version"`
	CipherSuite     string `json:"cipher_suite"`
	ALPN            string `json:"alpn"`
	SupportsSession bool   `json:"supports_session_ticket"`
	OCSPStapled     bool   `json:"ocsp_stapled"`

	// ServerHello 的扩展类型序列（顺序也记 —— JA3S 把它们按顺序入哈希）。
	ServerExtensions []uint16 `json:"server_extensions"`
	// 我们发出的 ClientHello 指纹（上游方向）。
	ClientExtensions []uint16 `json:"client_extensions"`
	ClientCiphers    []uint16 `json:"client_ciphers"`
	ClientCurves     []uint16 `json:"client_curves"`

	JA3SLine string `json:"ja3s_line"`
	JA3Line  string `json:"ja3_line"`

	// 证书链摘要。注意：**证书本身不算差异**（部署时会换成业务域名证书），
	// 这里记的是「链的形状」——它同样能被指纹化（自签 vs 公有 CA、链长度）。
	Certificates []CertSummary `json:"certificates"`
}

// CertSummary 是证书链的一环的摘要（不存全文，只存可比对的形状）。
type CertSummary struct {
	Subject      string `json:"subject"`
	Issuer       string `json:"issuer"`
	PublicKey    string `json:"public_key"`
	SigAlg       string `json:"sig_alg"`
	SANs         int    `json:"sans"`
	NotBefore    string `json:"not_before"`
	NotAfter     string `json:"not_after"`
	IsSelfSigned bool   `json:"is_self_signed"`
}

func main() {
	mode := flag.String("mode", "capture", "capture | diff")
	addr := flag.String("addr", "", "capture：目标 host:port")
	sni := flag.String("sni", "", "capture：SNI（留空则用 addr 的主机名）")
	out := flag.String("out", "", "capture：结果写到哪个 JSON 文件（留空则打印）")
	a := flag.String("a", "", "diff：A 组（真实站）JSON")
	b := flag.String("b", "", "diff：B 组（我方栈）JSON")
	timeout := flag.Duration("timeout", 10*time.Second, "单次握手的超时")
	flag.Parse()

	switch *mode {
	case "capture":
		if *addr == "" {
			fatal("capture 需要 -addr host:port")
		}
		cap, err := CaptureHandshake(*addr, *sni, *timeout)
		if err != nil {
			fatal("采集失败：%v", err)
		}
		writeCapture(*out, cap)
	case "diff":
		if *a == "" || *b == "" {
			fatal("diff 需要 -a 与 -b 两个 JSON")
		}
		ra, err := readCapture(*a)
		if err != nil {
			fatal("读 A 失败：%v", err)
		}
		rb, err := readCapture(*b)
		if err != nil {
			fatal("读 B 失败：%v", err)
		}
		report := Diff(ra, rb)
		fmt.Print(report.Render())
		if report.Verdict == VerdictCannotAlign {
			os.Exit(2) // 用退出码表达「命题需要重估」，便于脚本/CI 判定
		}
	default:
		fatal("未知 -mode=%q（可用 capture | diff）", *mode)
	}
}

// CaptureHandshake 与目标做一次 TLS 握手并采集指纹。
//
// 采集方式：把 TCP 连接包一层**双向录制**，自己解析我们发出的 ClientHello 与收到的 ServerHello ——
// 因为 Go 的 crypto/tls 不暴露扩展序列，而扩展顺序正是 JA3/JA3S 的一部分。
func CaptureHandshake(addr, sni string, timeout time.Duration) (*Capture, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("地址必须是 host:port：%w", err)
	}
	if sni == "" {
		sni = host
	}

	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	rc := newRecordingConn(conn)
	tlsConn := tls.Client(rc, &tls.Config{
		ServerName: sni,
		MinVersion: tls.VersionTLS12,
		// 采集本身不做校验：我们要看的是**形状**，不是信任关系（自签证书也要能采）。
		InsecureSkipVerify: true,
	})
	defer func() { _ = tlsConn.Close() }()
	_ = tlsConn.SetDeadline(time.Now().Add(timeout))

	if err := tlsConn.Handshake(); err != nil {
		return nil, fmt.Errorf("握手失败：%w", err)
	}
	state := tlsConn.ConnectionState()

	cap := &Capture{
		Target:      addr,
		SNI:         sni,
		CapturedAt:  time.Now().UTC(),
		ToolNote:    "scripts/fingerprint（E2：TLS 指纹一致性）",
		TLSVersion:  tlsVersionName(state.Version),
		CipherSuite: tls.CipherSuiteName(state.CipherSuite),
		ALPN:        state.NegotiatedProtocol,
		// 服务端是否发放会话票据：Go 在 TLS1.3 用 session ticket 恢复，DidResume 只反映本次是否恢复；
		// 这里记「服务端给了票据」的近似判据 = 客户端缓存里有票据可用。
		SupportsSession: tlsConn.ConnectionState().DidResume || len(state.PeerCertificates) > 0,
		OCSPStapled:     len(state.OCSPResponse) > 0,
	}

	// 解析双向录制的字节。
	if hs, perr := parseClientHello(rc.sentBytes()); perr == nil {
		cap.ClientExtensions = hs.extensionTypes()
		cap.ClientCiphers = hs.cipherSuites
		cap.ClientCurves = hs.supportedGroups
		cap.JA3Line = ja3Line(hs)
	}
	if hs, perr := parseServerHello(rc.receivedBytes()); perr == nil {
		cap.ServerExtensions = hs.extensionTypes()
		cap.JA3SLine = ja3sLine(state.Version, state.CipherSuite, hs.extensionTypes())
	}

	for _, c := range state.PeerCertificates {
		cap.Certificates = append(cap.Certificates, summarizeCert(c))
	}
	return cap, nil
}

func writeCapture(path string, cap *Capture) {
	data, err := json.MarshalIndent(cap, "", "  ")
	if err != nil {
		fatal("序列化失败：%v", err)
	}
	if path == "" {
		fmt.Println(string(data))
		return
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		fatal("写文件失败：%v", err)
	}
	fmt.Printf("已写入 %s（TLS %s · %s · ALPN %q）\n\tJA3S: %s\n",
		path, cap.TLSVersion, cap.CipherSuite, cap.ALPN, cap.JA3SLine)
}

func readCapture(path string) (*Capture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Capture
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fingerprint: "+format+"\n", args...)
	os.Exit(1)
}
