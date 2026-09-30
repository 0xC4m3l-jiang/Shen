package main

import (
	"encoding/binary"
	"strings"
	"testing"
)

// ── 夹具：手搓 ClientHello / ServerHello 字节 ────────────────────────────────
//
// 用固定字节而不是真实抓包：解析器是纯函数，夹具能让每个字段都被断言到，
// 且不受网络与对端实现变化影响。

func tlsRecord(handshake []byte) []byte {
	out := []byte{recordTypeHandshake, 3, 3}
	out = binary.BigEndian.AppendUint16(out, uint16(len(handshake)))
	return append(out, handshake...)
}

func handshake(msgType byte, body []byte) []byte {
	out := []byte{msgType}
	out = append(out, byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	return append(out, body...)
}

func ext(t uint16, body []byte) []byte {
	out := binary.BigEndian.AppendUint16(nil, t)
	out = binary.BigEndian.AppendUint16(out, uint16(len(body)))
	return append(out, body...)
}

// clientHelloFixture 造一个 TLS1.2（legacy 0x0303）的 ClientHello：
// 两个密码套件、三个扩展（supported_versions=43 / supported_groups=10 / ec_point_formats=11）。
func clientHelloFixture() []byte {
	var b []byte
	b = binary.BigEndian.AppendUint16(b, 0x0303) // legacy_version
	b = append(b, make([]byte, 32)...)           // random
	b = append(b, 0)                             // session_id 长度 0
	b = binary.BigEndian.AppendUint16(b, 4)      // cipher_suites 长度
	b = binary.BigEndian.AppendUint16(b, 0x1301) // TLS_AES_128_GCM_SHA256
	b = binary.BigEndian.AppendUint16(b, 0xc02f) // ECDHE_RSA_AES128_GCM_SHA256
	b = append(b, 1, 0)                          // compression

	var exts []byte
	exts = append(exts, ext(43, []byte{2, 0x03, 0x04})...) // supported_versions
	groups := binary.BigEndian.AppendUint16(nil, 4)        // 列表长度
	groups = binary.BigEndian.AppendUint16(groups, 0x001d) // x25519
	groups = binary.BigEndian.AppendUint16(groups, 0x0017) // secp256r1
	exts = append(exts, ext(10, groups)...)
	exts = append(exts, ext(11, []byte{1, 0})...) // ec_point_formats: uncompressed

	b = binary.BigEndian.AppendUint16(b, uint16(len(exts)))
	b = append(b, exts...)
	return tlsRecord(handshake(handshakeClientHello, b))
}

// serverHelloFixture 造一个 ServerHello：扩展顺序刻意与客户端不同（12 → 43）。
func serverHelloFixture() []byte {
	var b []byte
	b = binary.BigEndian.AppendUint16(b, 0x0303)
	b = append(b, make([]byte, 32)...)
	b = append(b, 0)                             // session_id
	b = binary.BigEndian.AppendUint16(b, 0x1301) // cipher
	b = append(b, 0)                             // compression

	var exts []byte
	exts = append(exts, ext(12, nil)...)                   // signature_algorithms（真实服务端常见）
	exts = append(exts, ext(43, []byte{2, 0x03, 0x04})...) // supported_versions
	b = binary.BigEndian.AppendUint16(b, uint16(len(exts)))
	b = append(b, exts...)
	return tlsRecord(handshake(handshakeServerHello, b))
}

// ── 解析 ─────────────────────────────────────────────────────────────────────

func TestParseClientHello(t *testing.T) {
	h, err := parseClientHello(clientHelloFixture())
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if h.legacyVersion != 0x0303 {
		t.Errorf("legacy_version 应为 0x0303，实际 0x%04x", h.legacyVersion)
	}
	if len(h.cipherSuites) != 2 || h.cipherSuites[0] != 0x1301 {
		t.Errorf("密码套件解析错误：%v", h.cipherSuites)
	}
	if got := joinUint16(h.extensionTypes(), false); got != "43-10-11" {
		t.Errorf("扩展顺序应原样保留：%s", got)
	}
	if got := joinUint16(h.supportedGroups, false); got != "29-23" {
		t.Errorf("supported_groups 解析错误：%s", got)
	}
	if len(h.pointFormats) != 1 || h.pointFormats[0] != 0 {
		t.Errorf("ec_point_formats 解析错误：%v", h.pointFormats)
	}
}

func TestParseServerHello(t *testing.T) {
	h, err := parseServerHello(serverHelloFixture())
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if got := joinUint16(h.extensionTypes(), false); got != "12-43" {
		t.Errorf("服务端扩展顺序应原样保留：%s", got)
	}
	if len(h.cipherSuites) != 1 || h.cipherSuites[0] != 0x1301 {
		t.Errorf("选用套件解析错误：%v", h.cipherSuites)
	}
}

func TestParseToleratesTruncatedTail(t *testing.T) {
	raw := clientHelloFixture()
	// 砍掉尾部若干字节：模拟「握手刚完成就关闭连接」时录到的半条记录。
	if _, err := parseClientHello(raw[:len(raw)-6]); err != nil {
		t.Fatalf("尾部不完整时仍应尽量解析：%v", err)
	}
	if _, err := parseClientHello([]byte{0, 1, 2}); err == nil {
		t.Error("完全不是 TLS 的字节应当报错")
	}
}

// ── 指纹原文（JA3 / JA3S）────────────────────────────────────────────────────

func TestJA3AndJA3SLines(t *testing.T) {
	ch, err := parseClientHello(clientHelloFixture())
	if err != nil {
		t.Fatal(err)
	}
	line := ja3Line(ch)
	if !strings.HasPrefix(line, "771,") { // 0x0303 = 771
		t.Errorf("JA3 首段应为 SSLVersion=771：%s", line)
	}
	if !strings.Contains(line, "4865-49199") { // 0x1301 / 0xc02f
		t.Errorf("JA3 应含密码套件十进制序列：%s", line)
	}
	if !strings.Contains(line, "43-10-11") {
		t.Errorf("JA3 应含扩展序列：%s", line)
	}

	sh, err := parseServerHello(serverHelloFixture())
	if err != nil {
		t.Fatal(err)
	}
	got := ja3sLine(0x0304, 0x1301, sh.extensionTypes())
	if got != "772,4865,12-43" {
		t.Errorf("JA3S 应为 772,4865,12-43，实际 %s", got)
	}
}

func TestGREASEFilterOnlyForJA3S(t *testing.T) {
	if !isGREASE(0x0a0a) || !isGREASE(0xfafa) {
		t.Error("0x0a0a / 0xfafa 是 GREASE")
	}
	if isGREASE(0x1301) || isGREASE(0x0a0b) {
		t.Error("非 GREASE 值被误判")
	}
	// JA3 不过滤、JA3S 过滤：同一个集合在两条口径下结果不同 —— 这是刻意的（规范如此）。
	exts := []uint16{0x0a0a, 12}
	if joinUint16(exts, false) != "2570-12" {
		t.Error("JA3 口径不该过滤 GREASE")
	}
	if joinUint16(exts, true) != "12" {
		t.Error("JA3S 口径必须过滤 GREASE")
	}
}

// ── 对比与判定（E2 的三种结论）───────────────────────────────────────────────

func baseCapture() *Capture {
	return &Capture{
		Target: "real:443", TLSVersion: "TLS 1.3", CipherSuite: "TLS_AES_128_GCM_SHA256", ALPN: "h2",
		ServerExtensions: []uint16{43, 51}, SupportsSession: true, OCSPStapled: true,
		Certificates: []CertSummary{{Subject: "CN=real", Issuer: "CN=CA", PublicKey: "*ecdsa.PublicKey", SigAlg: "SHA256-RSA", SANs: 2}},
		JA3SLine:     "772,4865,43-51",
	}
}

func TestDiffIndistinguishable(t *testing.T) {
	a, b := baseCapture(), baseCapture()
	b.Target = "ours:8443"
	rep := Diff(a, b)
	if rep.Verdict != VerdictIndistinguishable {
		t.Fatalf("完全一致时应为「不可区分」，实际 %q（差异 %+v）", rep.Verdict, rep.Differences)
	}
}

func TestDiffConfigurableOnly(t *testing.T) {
	a := baseCapture()
	b := baseCapture()
	b.ALPN = "http/1.1"   // 可配置项
	b.OCSPStapled = false // 可配置项
	rep := Diff(a, b)
	if rep.Verdict != VerdictConfigurable {
		t.Fatalf("只差可配置项时应为「可区分（可调齐）」，实际 %q", rep.Verdict)
	}
	for _, d := range rep.Differences {
		if d.Class == ClassEssential {
			t.Errorf("不该出现本质差异：%+v", d)
		}
	}
}

func TestDiffCannotAlignOnExtensionOrder(t *testing.T) {
	a := baseCapture()
	b := baseCapture()
	b.ServerExtensions = []uint16{51, 43} // 同一集合、不同顺序 —— 这正是 JA3S 能看出来的
	b.JA3SLine = "772,4865,51-43"
	rep := Diff(a, b)
	if rep.Verdict != VerdictCannotAlign {
		t.Fatalf("扩展顺序不同应判「不可对齐」，实际 %q", rep.Verdict)
	}
	if !strings.Contains(rep.Render(), "威胁模型 R-1") {
		t.Error("不可对齐的报告必须点出 R-1 与后续动作")
	}
}

func TestDiffCertOnlyIsStillIndistinguishable(t *testing.T) {
	a := baseCapture()
	b := baseCapture()
	b.Certificates = []CertSummary{{Subject: "CN=shop.example.com", Issuer: "CN=Let's Encrypt", PublicKey: "*rsa.PublicKey", SigAlg: "SHA256-RSA", SANs: 1, IsSelfSigned: true}}
	rep := Diff(a, b)
	if rep.Verdict != VerdictIndistinguishable {
		t.Fatalf("只差证书形状不应判为「可区分」（部署时会替换证书），实际 %q", rep.Verdict)
	}
}
