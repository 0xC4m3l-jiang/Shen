package main

import (
	"bytes"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── 采集：双向录制的连接 ──────────────────────────────────────────────────────

// recordingConn 把 TCP 连接包一层，**双向录制**字节。
//
// 为什么不用 Go 的 tls.ConnectionState 就够了：它**不暴露扩展序列**，
// 而扩展的「有哪些、什么顺序」正是 JA3 / JA3S 的核心输入，也是攻击者最容易比对的形状。
type recordingConn struct {
	net.Conn
	mu           sync.Mutex
	sentBytesBuf bytes.Buffer
	recvBytesBuf bytes.Buffer
}

func newRecordingConn(c net.Conn) *recordingConn {
	return &recordingConn{Conn: c}
}

func (c *recordingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		c.mu.Lock()
		c.sentBytesBuf.Write(b[:n])
		c.mu.Unlock()
	}
	return n, err
}

func (c *recordingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.mu.Lock()
		c.recvBytesBuf.Write(b[:n])
		c.mu.Unlock()
	}
	return n, err
}

func (c *recordingConn) sentBytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.sentBytesBuf.Bytes()...)
}

func (c *recordingConn) receivedBytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.recvBytesBuf.Bytes()...)
}

// ── 解析：从原始字节里取握手消息 ──────────────────────────────────────────────

const (
	recordTypeHandshake  = 22
	handshakeClientHello = 1
	handshakeServerHello = 2
)

// handshakeMessages 从原始 TLS 字节流里抽出指定类型的握手消息体。
//
// 容错：录制到的字节可能以**半条记录**结尾（握手刚完成、或对端随后关闭），
// 这里按「能解析多少算多少」，不因为尾部不完整就整体失败。
func handshakeMessages(raw []byte, msgType uint8) ([][]byte, error) {
	var stream []byte
	for i := 0; i+5 <= len(raw); {
		recType := raw[i]
		recLen := int(binary.BigEndian.Uint16(raw[i+3 : i+5]))
		i += 5
		if i+recLen > len(raw) {
			recLen = len(raw) - i
		}
		if recType == recordTypeHandshake {
			stream = append(stream, raw[i:i+recLen]...)
		}
		i += recLen
	}

	var out [][]byte
	for j := 0; j+4 <= len(stream); {
		t := stream[j]
		l := int(stream[j+1])<<16 | int(stream[j+2])<<8 | int(stream[j+3])
		j += 4
		if j+l > len(stream) {
			// 尾部不完整（录制到的字节被截断）。与「报错放弃」相比，**按能拿到的部分解析**
			// 对取证工具更有用：我们要的是形状，不是完整性证明；越界的读取由各解析处自行夹紧。
			if t == msgType {
				out = append(out, stream[j:])
			}
			break
		}
		if t == msgType {
			out = append(out, stream[j:j+l])
		}
		j += l
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没找到握手消息 type=%d（录到的字节可能不完整）", msgType)
	}
	return out, nil
}

// extEntry 是一个扩展：类型 + 序号（顺序本身是指纹的一部分）。
type extEntry struct {
	Type   uint16
	Length int
}

// helloInfo 是从 ClientHello / ServerHello 里解析出来的可比对字段。
type helloInfo struct {
	legacyVersion   uint16
	cipherSuites    []uint16
	extensions      []extEntry
	supportedGroups []uint16
	pointFormats    []uint16
}

func (h *helloInfo) extensionTypes() []uint16 {
	out := make([]uint16, 0, len(h.extensions))
	for _, e := range h.extensions {
		out = append(out, e.Type)
	}
	return out
}

// parseClientHello 解析我们**发出**的 ClientHello。
func parseClientHello(raw []byte) (*helloInfo, error) {
	msgs, err := handshakeMessages(raw, handshakeClientHello)
	if err != nil {
		return nil, err
	}
	b := msgs[0]
	if len(b) < 34 {
		return nil, fmt.Errorf("ClientHello 太短：%d 字节", len(b))
	}
	h := &helloInfo{legacyVersion: binary.BigEndian.Uint16(b[0:2])}

	p := 34 // legacy_version(2) + random(32)
	if p >= len(b) {
		return nil, fmt.Errorf("ClientHello 缺 session_id 长度")
	}
	sidLen := int(b[p])
	p += 1 + sidLen

	if p+2 > len(b) {
		return nil, fmt.Errorf("ClientHello 缺 cipher_suites 长度")
	}
	csLen := int(binary.BigEndian.Uint16(b[p : p+2]))
	p += 2
	for k := 0; k+2 <= csLen && p+2 <= len(b); k += 2 {
		h.cipherSuites = append(h.cipherSuites, binary.BigEndian.Uint16(b[p:p+2]))
		p += 2
	}

	if p >= len(b) {
		return nil, fmt.Errorf("ClientHello 缺 compression_methods 长度")
	}
	compLen := int(b[p])
	p += 1 + compLen

	if p+2 > len(b) {
		return h, nil // 没有扩展（老客户端）：合法
	}
	extTotal := int(binary.BigEndian.Uint16(b[p : p+2]))
	p += 2
	end := p + extTotal
	if end > len(b) {
		end = len(b)
	}
	for p+4 <= end {
		et := binary.BigEndian.Uint16(b[p : p+2])
		el := int(binary.BigEndian.Uint16(b[p+2 : p+4]))
		bodyEnd := p + 4 + el
		if bodyEnd > end {
			bodyEnd = end
		}
		body := b[p+4 : bodyEnd]
		h.extensions = append(h.extensions, extEntry{Type: et, Length: el})
		switch et {
		case 10: // supported_groups
			h.supportedGroups = parseUint16List(body)
		case 11: // ec_point_formats
			h.pointFormats = parseByteList(body)
		}
		p = bodyEnd
	}
	return h, nil
}

// parseServerHello 解析对端**发来**的 ServerHello。
func parseServerHello(raw []byte) (*helloInfo, error) {
	msgs, err := handshakeMessages(raw, handshakeServerHello)
	if err != nil {
		return nil, err
	}
	b := msgs[0]
	if len(b) < 38 {
		return nil, fmt.Errorf("ServerHello 太短：%d 字节", len(b))
	}
	h := &helloInfo{legacyVersion: binary.BigEndian.Uint16(b[0:2])}

	p := 34
	sidLen := int(b[p])
	p += 1 + sidLen
	if p+3 > len(b) {
		return h, nil
	}
	h.cipherSuites = []uint16{binary.BigEndian.Uint16(b[p : p+2])}
	p += 2
	p++ // compression_method

	if p+2 > len(b) {
		return h, nil
	}
	extTotal := int(binary.BigEndian.Uint16(b[p : p+2]))
	p += 2
	end := p + extTotal
	if end > len(b) {
		end = len(b)
	}
	for p+4 <= end {
		et := binary.BigEndian.Uint16(b[p : p+2])
		el := int(binary.BigEndian.Uint16(b[p+2 : p+4]))
		h.extensions = append(h.extensions, extEntry{Type: et, Length: el})
		p += 4 + el
	}
	return h, nil
}

func parseUint16List(b []byte) []uint16 {
	if len(b) < 2 {
		return nil
	}
	n := int(binary.BigEndian.Uint16(b[0:2]))
	var out []uint16
	for i := 2; i+2 <= len(b) && i-2 < n; i += 2 {
		out = append(out, binary.BigEndian.Uint16(b[i:i+2]))
	}
	return out
}

func parseByteList(b []byte) []uint16 {
	if len(b) == 0 {
		return nil
	}
	n := int(b[0])
	var out []uint16
	for i := 1; i < len(b) && i-1 < n; i++ {
		out = append(out, uint16(b[i]))
	}
	return out
}

// ── 指纹：JA3 / JA3S ─────────────────────────────────────────────────────────

// isGREASE 判断 GREASE 值（0x0a0a / 0x1a1a … 0xfafa）：它们是对抗中间盒的填充，**不是指纹**。
func isGREASE(v uint16) bool {
	if byte(v>>8) != byte(v) {
		return false
	}
	return byte(v)&0x0f == 0x0a
}

func joinUint16(vs []uint16, dropGREASE bool) string {
	parts := make([]string, 0, len(vs))
	for _, v := range vs {
		if dropGREASE && isGREASE(v) {
			continue
		}
		parts = append(parts, strconv.Itoa(int(v)))
	}
	return strings.Join(parts, "-")
}

// ja3Line 按 JA3 口径拼出客户端指纹的**原文**（不在这里算哈希）。
//
// 口径（Salesforce）：`SSLVersion,Cipher,SSLExtension,EllipticCurve,EllipticCurvePointFormat`。
// 每段内多值用 `-` 连接；JA3 **不过滤 GREASE**（这是它被诟病的点；本工具自身的 Go 客户端不发 GREASE）。
//
// 为什么输出原文而不是 MD5：比对时要看的是**差在哪一项**，哈希只能告诉你「不一样」。
// 需要与外部工具（ja3er 等）对账时，算哈希是一行命令：
//
//	printf '%s' "<ja3_line>" | md5        # macOS/Linux 均可
func ja3Line(h *helloInfo) string {
	if h == nil {
		return ""
	}
	return strings.Join([]string{
		strconv.Itoa(int(h.legacyVersion)),
		joinUint16(h.cipherSuites, false),
		joinUint16(h.extensionTypes(), false),
		joinUint16(h.supportedGroups, false),
		joinUint16(h.pointFormats, false),
	}, ",")
}

// ja3sLine 按 JA3S 口径拼出服务端指纹的**原文**：`SSLVersion,Cipher,SSLExtension`。
// JA3S 口径**过滤 GREASE**（它与 JA3 的口径差异就在这里，别混用）。
func ja3sLine(version, cipher uint16, exts []uint16) string {
	return strings.Join([]string{
		strconv.Itoa(int(version)),
		strconv.Itoa(int(cipher)),
		joinUint16(exts, true),
	}, ",")
}

// ── 杂项 ─────────────────────────────────────────────────────────────────────

// tlsVersionName 把协商版本变成可读名字（差异报告里给人看）。
func tlsVersionName(v uint16) string {
	switch v {
	case 0x0301:
		return "TLS 1.0"
	case 0x0302:
		return "TLS 1.1"
	case 0x0303:
		return "TLS 1.2"
	case 0x0304:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("0x%04x", v)
	}
}

// summarizeCert 抽取证书链的**形状**（不存全文）。
//
// 证书内容本身不算差异 —— 部署时会换成业务域名证书；能指纹化的是链的形状
// （自签 vs 公有 CA、几级链、密钥类型、签名算法、SAN 数量）。
func summarizeCert(c *x509.Certificate) CertSummary {
	selfSigned := c.Subject.String() == c.Issuer.String() && c.CheckSignatureFrom(c) == nil
	return CertSummary{
		Subject:      c.Subject.String(),
		Issuer:       c.Issuer.String(),
		PublicKey:    fmt.Sprintf("%T", c.PublicKey),
		SigAlg:       c.SignatureAlgorithm.String(),
		SANs:         len(c.DNSNames) + len(c.IPAddresses),
		NotBefore:    c.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:     c.NotAfter.UTC().Format(time.RFC3339),
		IsSelfSigned: selfSigned,
	}
}
