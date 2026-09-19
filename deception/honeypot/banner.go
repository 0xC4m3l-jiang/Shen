package honeypot

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// Banner 是一个**最小但真实**的协议适配器：接受连接 → 逐行问候 → 读对手输入并录制。
//
// 它有两个作用：
//  1. 示范框架怎么用（握手 → 读交互 → 录制 → 结束），让 `Runner` 与 `Session` 契约可跑、可测；
//  2. 本身就是可用的**协议门牌**仿真（SSH / FTP / SMTP 这类协议的第一步都是 banner 交换）。
//
// **核心逻辑接缝**：真实协议栈（SSH 密钥交换 / MySQL 握手 / Redis RESP / 凭证捕获与命令解释）
// 不在这里 —— 那是「可选自研」的部分（[ADR-0011](../../docs/background/decisions/0011-honeypot-entry-external-backends.md)），
// 默认接第三方蜜罐。新协议实现只需满足 `Protocol`，然后 `Register` 进来。
type Banner struct {
	// ProtocolName 是协议名（与注册表键一致）。
	ProtocolName string
	// Port 是默认端口（仅供部署参考）。
	Port int
	// Lines 是逐行发出的问候语。
	Lines []string
	// MaxLines 是读完多少行后结束会话；0 → DefaultBannerLines。
	MaxLines int
	// ReadTimeout 是两次读之间的空闲上限；0 → DefaultBannerReadTimeout。
	ReadTimeout time.Duration
}

const (
	// DefaultBannerLines 是单个会话最多读取的行数。
	//
	// 必须有上限：蜜罐是一个**被动**组件（`SB-6`），不能让对手用「一直说话」把它喂死。
	DefaultBannerLines = 16
	// DefaultBannerReadTimeout 是读空闲上限。超过即认为对手不说话了，安静结束会话。
	DefaultBannerReadTimeout = 30 * time.Second
)

// Name 实现 Protocol。
func (b Banner) Name() string { return b.ProtocolName }

// DefaultPort 实现 Protocol。
func (b Banner) DefaultPort() int { return b.Port }

// Serve 实现 Protocol：先问候，再逐行读取并录制，直到对手断开、读超时或读满 MaxLines。
//
// 语义约定：
//   - 对手断开（EOF）或读超时 → **安静结束**（返回 nil）：蜜罐不该因为对手不配合而刷日志；
//   - 写失败（对端已关）→ 返回错误，由运行框架决定记录方式；
//   - ctx 取消（框架停止）→ 返回 ctx.Err()。
func (b Banner) Serve(ctx context.Context, conn net.Conn, sess Session) error {
	for _, line := range b.Lines {
		sess.Transcript(DirectionOut, line)
		if _, err := fmt.Fprintf(conn, "%s\r\n", line); err != nil {
			return fmt.Errorf("honeypot: 协议 %s 写问候失败：%w", b.ProtocolName, err)
		}
	}

	maxLines := b.MaxLines
	if maxLines <= 0 {
		maxLines = DefaultBannerLines
	}
	timeout := b.ReadTimeout
	if timeout <= 0 {
		timeout = DefaultBannerReadTimeout
	}

	reader := bufio.NewReader(conn)
	for i := 0; i < maxLines; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		line, err := reader.ReadString('\n')
		if trimmed := strings.TrimRight(line, "\r\n"); trimmed != "" {
			sess.Transcript(DirectionIn, trimmed)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil // 对手正常断开
			}
			// 读超时 / 连接被关：都按「会话自然结束」处理，不当错误上报。
			return nil
		}
	}
	return nil
}
