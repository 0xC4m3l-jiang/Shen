// Package geoip 把来源 IP 解析成**中文归属地**（ip2region 离线库），并区分内网 / 回环 / 保留地址。
//
// 为什么自写读取器而不引入官方 binding：xdb 查询只是「向量索引 + 段索引二分」约 60 行标准库代码；
// 自写可以避免新增第三方 Go 模块（vendor 与许可台账都不用动），也能按我们的需要做「只读共享缓冲、
// 并发安全、坏文件启动即报错」。数据文件本身来自 ip2region（Apache-2.0 OR MIT，见 data/LICENSE.ip2region）。
//
// 数据结构（xdb v3，IPv4）：
//
//	[0,256)                 头部：结构版本、索引策略、生成时间、段索引起止指针、IP 版本、指针字节数
//	[256, 256+256*256*8)    向量索引：按 IP 前两个字节定位段索引区间（起止指针，各 4 字节小端）
//	段索引                   每条 14 字节：起始 IP(4, 小端) · 结束 IP(4, 小端) · 数据长度(2) · 数据指针(4)
//	区域数据                 UTF-8：`国家|省份|城市|ISP|ISO`，`0` 表示未知
package geoip

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
)

//go:embed data/ip2region_v4.xdb
var embedded []byte

const (
	headerLen     = 256
	vectorCols    = 256
	vectorEntry   = 8
	vectorLen     = 256 * vectorCols * vectorEntry
	segmentLen    = 14
	unknownRegion = "0"
)

// Scope 是地址类别。
type Scope string

// 地址类别取值（前端按它显示不同标签）。
const (
	ScopePublic   Scope = "public"
	ScopePrivate  Scope = "private"  // RFC1918 / ULA / CGNAT / 链路本地
	ScopeLoopback Scope = "loopback" // 127/8、::1
	ScopeReserved Scope = "reserved" // 文档、组播、未指定、保留段
	ScopeUnknown  Scope = "unknown"  // 无法解析 / 库中无记录
)

// Location 是一次解析结果。
type Location struct {
	Country  string `json:"country"`
	Province string `json:"province"`
	City     string `json:"city"`
	ISP      string `json:"isp"`
	ISO      string `json:"iso"`
	Scope    Scope  `json:"scope"`
	Label    string `json:"label"` // 页面直接展示的一行文字
}

// DB 是只读的归属地库；Lookup 并发安全（只读共享缓冲）。
type DB struct {
	buf      []byte
	built    uint32
	segStart uint32
	segEnd   uint32
}

// Default 打开内嵌的数据文件。
func Default() (*DB, error) { return Open(embedded) }

// OpenFile 打开外部数据文件（用于替换为更新的库）。
func OpenFile(path string) (*DB, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("geoip: 读取 %s 失败：%w", path, err)
	}
	return Open(raw)
}

// Open 校验并装载 xdb 缓冲。坏文件在启动时报错，而不是在运行中给出错误的归属地。
func Open(buf []byte) (*DB, error) {
	if len(buf) < headerLen+vectorLen {
		return nil, errors.New("geoip: 数据文件过短，不是合法的 xdb")
	}
	ipVersion := binary.LittleEndian.Uint16(buf[16:])
	ptrBytes := binary.LittleEndian.Uint16(buf[18:])
	if (ipVersion != 0 && ipVersion != 4) || (ptrBytes != 0 && ptrBytes != 4) {
		return nil, fmt.Errorf("geoip: 只支持 IPv4 xdb（文件声明 ip_version=%d, ptr_bytes=%d）", ipVersion, ptrBytes)
	}
	db := &DB{
		buf:      buf,
		built:    binary.LittleEndian.Uint32(buf[4:]),
		segStart: binary.LittleEndian.Uint32(buf[8:]),
		segEnd:   binary.LittleEndian.Uint32(buf[12:]),
	}
	if db.segStart < headerLen+vectorLen || db.segEnd < db.segStart ||
		int(db.segEnd)+segmentLen > len(buf) || (db.segEnd-db.segStart)%segmentLen != 0 {
		return nil, errors.New("geoip: 段索引指针越界，数据文件已损坏")
	}
	return db, nil
}

// BuiltAt 返回数据文件的生成时间（Unix 秒），用于页面标注「归属地库版本」。
func (db *DB) BuiltAt() uint32 { return db.built }

// Lookup 解析一个 IP 字符串。
func (db *DB) Lookup(raw string) Location {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return Location{Scope: ScopeUnknown, Label: "未知来源"}
	}
	addr = addr.Unmap()
	if scope, label, special := classify(addr); special {
		return Location{Scope: scope, Label: label}
	}
	if !addr.Is4() {
		return Location{Scope: ScopePublic, Label: "公网 IPv6（离线库暂不覆盖）"}
	}
	region, ok := db.search(addr.As4())
	if !ok {
		return Location{Scope: ScopeUnknown, Label: "未收录"}
	}
	return parseRegion(region)
}

func (db *DB) search(ip [4]byte) (string, bool) {
	idx := headerLen + int(ip[0])*vectorCols*vectorEntry + int(ip[1])*vectorEntry
	sPtr := binary.LittleEndian.Uint32(db.buf[idx:])
	ePtr := binary.LittleEndian.Uint32(db.buf[idx+4:])
	if sPtr == 0 || ePtr == 0 || ePtr < sPtr || int(ePtr)+segmentLen > len(db.buf) {
		return "", false
	}
	value := binary.BigEndian.Uint32(ip[:])
	lo, hi := 0, int((ePtr-sPtr)/segmentLen)
	for lo <= hi {
		mid := (lo + hi) >> 1
		p := int(sPtr) + mid*segmentLen
		start := binary.LittleEndian.Uint32(db.buf[p:])
		end := binary.LittleEndian.Uint32(db.buf[p+4:])
		switch {
		case value < start:
			hi = mid - 1
		case value > end:
			lo = mid + 1
		default:
			dataLen := int(binary.LittleEndian.Uint16(db.buf[p+8:]))
			dataPtr := int(binary.LittleEndian.Uint32(db.buf[p+10:]))
			if dataLen == 0 || dataPtr+dataLen > len(db.buf) {
				return "", false
			}
			return string(db.buf[dataPtr : dataPtr+dataLen]), true
		}
	}
	return "", false
}

func parseRegion(region string) Location {
	fields := strings.Split(region, "|")
	get := func(i int) string {
		if i < len(fields) && fields[i] != unknownRegion {
			return strings.TrimSpace(fields[i])
		}
		return ""
	}
	loc := Location{Country: get(0), Province: get(1), City: get(2), ISP: get(3), ISO: get(4), Scope: ScopePublic}
	if strings.EqualFold(loc.Country, "reserved") {
		return Location{Scope: ScopeReserved, Label: "保留地址"}
	}
	parts := make([]string, 0, 3)
	for _, p := range []string{loc.Country, loc.Province, loc.City} {
		if p != "" && (len(parts) == 0 || parts[len(parts)-1] != p) {
			parts = append(parts, p)
		}
	}
	loc.Label = strings.Join(parts, " · ")
	if loc.Label == "" {
		loc.Label = "未收录"
	}
	return loc
}

var (
	cgnat      = netip.MustParsePrefix("100.64.0.0/10")
	benchmark  = netip.MustParsePrefix("198.18.0.0/15")
	reserved4  = netip.MustParsePrefix("240.0.0.0/4")
	thisNet    = netip.MustParsePrefix("0.0.0.0/8")
	docs       = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8::/32")}
	protoAssig = netip.MustParsePrefix("192.0.0.0/24")
)

// classify 识别不需要查库的特殊地址。
func classify(a netip.Addr) (Scope, string, bool) {
	switch {
	case a.IsLoopback():
		return ScopeLoopback, "本机回环", true
	case a.IsPrivate() || cgnat.Contains(a):
		return ScopePrivate, "内网地址", true
	case a.IsLinkLocalUnicast():
		return ScopePrivate, "链路本地", true
	case a.IsUnspecified() || a.IsMulticast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast():
		return ScopeReserved, "保留地址", true
	case a.Is4() && (reserved4.Contains(a) || thisNet.Contains(a) || benchmark.Contains(a) || protoAssig.Contains(a)):
		return ScopeReserved, "保留地址", true
	}
	for _, p := range docs {
		if p.Contains(a) {
			return ScopeReserved, "文档示例地址", true
		}
	}
	return "", "", false
}
