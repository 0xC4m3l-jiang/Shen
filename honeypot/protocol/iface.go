// Package honeypot 是 L2 的**协议仿真**层：承接幻境路由来的流量，在假协议栈上与对手交互，
// 并把每一次交互录制成事件（会话转录 / 凭证捕获 / 文件投递）。
//
// 依据 [ADR-0011](../../docs/background/decisions/0011-honeypot-entry-external-backends.md)：
// **本模块是可选自研**，默认接第三方蜜罐。因此这里交付的是**协议适配器契约与运行框架**：
//
//   - 框架部分在这里实现并单测：注册表 · 连接上限（`MD-16`）· 资源对称回收（`MD-15`）·
//     停止时的双层超时（`MD-14` 的内层）· 事件上行口（会话 ID 一等字段，`AR-25`）；
//   - **真实协议栈（SSH 密钥交换 / MySQL 握手 / …）是后续工作**，落在 `Protocol` 契约之后 ——
//     那是本模块的「核心逻辑」接缝，不与框架混写。
//
// 边界（不做的事）：
//   - **不做判定**（`AR-2`）· **不做决策**（`MD-12`）—— 那在核心；
//   - **不主动连接任何目标**（`SB-6`）：本模块只被动接受连接，没有任何出站拨号；
//   - **不与判别同进程**（`MD-4`）：它是独立的 L2 进程。
package honeypot

import (
	"context"
	"net"
	"time"
)

// Direction 是一次交互的方向。
type Direction string

const (
	// DirectionIn 是对手发来的内容（命令 / 查询）。
	DirectionIn Direction = "in"
	// DirectionOut 是我方回给对手的内容。
	DirectionOut Direction = "out"
)

// Session 是一次会话的**录制口**。
//
// 会话 ID 是**一等字段**（`AR-25`）：建立、心跳、收尾三阶段必须是同一个 ID，
// 否则转录会在存储里散成一堆互不相干的行，归因与攻击链还原都无从谈起。
type Session interface {
	// ID 返回会话标识（`AR-25`）。
	ID() string
	// Transcript 记一次交互。
	Transcript(direction Direction, line string)
	// Credential 记一次凭证捕获。
	Credential(user, secret string)
	// File 记一次文件投递：uploaded=true 表示对手上传（投递给我们）。
	File(name string, size int, uploaded bool)
}

// Protocol 是一个**协议适配器**：与对手完成握手，并解释其交互。
//
// 实现方约定：
//   - `Serve` 必须在 ctx 取消时尽快返回，且**不得**阻塞在无超时的读上；
//   - 任何内部错误都应通过返回值上报，由运行框架决定记录方式（框架不 panic、不吞错）。
type Protocol interface {
	// Name 是协议名（ssh / mysql / redis / ftp / elasticsearch / nginx-admin / web-clone …），
	// 与 `docs/modules/honeypot.md` §1 的类型清单一致。
	Name() string
	// DefaultPort 是该协议的默认监听端口（仅供装配参考，实际端口来自部署配置）。
	DefaultPort() int
	// Serve 在一条**已接受**的连接上服务到结束。
	Serve(ctx context.Context, conn net.Conn, sess Session) error
}

// SessionFactory 按连接创建会话录制口。
//
// 由消费方提供（运行框架只依赖这个接口）：生产实现会把转录经 `api/telemetry/v1` 上行，
// 单测用替身 —— 因此本模块**不** import 核心内部包（`ST-3` / `ST-4`）。
type SessionFactory interface {
	NewSession(protocol string, remote net.Addr) Session
}

// Registry 是协议适配器的注册表。
//
// 名字唯一：重复注册必须报错 —— 否则「哪个实现生效」取决于注册顺序，
// 排障时会出现「配置说 ssh、跑起来是别的」这种最难查的一类问题。
type Registry struct {
	protocols map[string]Protocol
}

// NewRegistry 返回空注册表。
func NewRegistry() *Registry {
	return &Registry{protocols: map[string]Protocol{}}
}

// Register 注册一个协议适配器。
func (r *Registry) Register(p Protocol) error {
	if p == nil {
		return errNilProtocol
	}
	name := p.Name()
	if name == "" {
		return errEmptyName
	}
	if _, dup := r.protocols[name]; dup {
		return &DuplicateError{Name: name}
	}
	r.protocols[name] = p
	return nil
}

// Lookup 按名字取协议适配器。
func (r *Registry) Lookup(name string) (Protocol, bool) {
	p, ok := r.protocols[name]
	return p, ok
}

// Names 返回已注册的协议名（顺序不保证，调用方可自行排序）。
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.protocols))
	for n := range r.protocols {
		out = append(out, n)
	}
	return out
}

// Stats 是一个协议的运行计数。
type Stats struct {
	// Active 是当前在途连接数。
	Active int
	// Accepted 是累计接受的连接数。
	Accepted uint64
	// Rejected 是因超限被拒的连接数（`MD-16`：超限即拒绝并记录）。
	Rejected uint64
}

// Limits 是运行期的硬约束。
type Limits struct {
	// MaxConnsPerProtocol 是每个协议的并发连接上限（`MD-16`）。0 → DefaultMaxConns。
	MaxConnsPerProtocol int
	// GracePeriod 是停止时留给在途连接的宽限期；到点强制关闭（`MD-14` 的双层超时的内层）。
	GracePeriod time.Duration
}

const (
	// DefaultMaxConns 是并发连接上限的默认值。它必须存在：没有上限的蜜罐等于给对手一个
	// 免费的资源耗尽入口（`MD-16`）。
	DefaultMaxConns = 64
	// DefaultGracePeriod 是停止时的默认宽限。
	DefaultGracePeriod = 5 * time.Second
)
