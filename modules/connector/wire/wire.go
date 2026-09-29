// Package wire 是反向隧道**控制信道**的线协议实现：4 字节大端长度前缀 + protobuf。
//
// 数据面（流 ≥ 1）没有协议 —— 那是裸字节管道；本包只管流 0 上的握手与会话事件。
// 网关（gateway 包）与连接器（sdk 包）共用这一份实现，避免两处各自漂移。
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	connectorv1 "shen/common/api/connector/v1"

	"google.golang.org/protobuf/proto"
)

// MaxFrame 控制帧上限（握手与事件都小；超限按坏协议处理）。
const MaxFrame = 64 << 10

// Write 发送一条控制消息（可选写超时）。
func Write(w io.Writer, msg proto.Message, timeout time.Duration) error {
	raw, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	if len(raw) > MaxFrame {
		return errors.New("控制消息超长")
	}
	if timeout > 0 {
		if tw, ok := w.(interface{ SetWriteDeadline(time.Time) error }); ok {
			_ = tw.SetWriteDeadline(time.Now().Add(timeout))
			defer func() { _ = tw.SetWriteDeadline(time.Time{}) }()
		}
	}
	var head [4]byte
	binary.BigEndian.PutUint32(head[:], uint32(len(raw)))
	if _, err := w.Write(head[:]); err != nil {
		return err
	}
	_, err = w.Write(raw)
	return err
}

// ReadHandshakeRequest 读握手请求（网关侧：流 0 首条消息）。
func ReadHandshakeRequest(r io.Reader, timeout time.Duration) (*connectorv1.HandshakeRequest, error) {
	buf, err := readFrame(r, timeout)
	if err != nil {
		return nil, err
	}
	var hs connectorv1.HandshakeRequest
	if err := proto.Unmarshal(buf, &hs); err != nil {
		return nil, fmt.Errorf("握手消息解码失败：%w", err)
	}
	return &hs, nil
}

// ReadHandshakeResponse 读握手应答（连接器侧）。
func ReadHandshakeResponse(r io.Reader, timeout time.Duration) (*connectorv1.HandshakeResponse, error) {
	buf, err := readFrame(r, timeout)
	if err != nil {
		return nil, err
	}
	var resp connectorv1.HandshakeResponse
	if err := proto.Unmarshal(buf, &resp); err != nil {
		return nil, fmt.Errorf("握手应答解码失败：%w", err)
	}
	return &resp, nil
}

// ReadEvent 读会话事件（网关侧：握手之后的控制流消息）。
func ReadEvent(r io.Reader, timeout time.Duration) (*connectorv1.SessionEvent, error) {
	buf, err := readFrame(r, timeout)
	if err != nil {
		return nil, err
	}
	var ev connectorv1.SessionEvent
	if err := proto.Unmarshal(buf, &ev); err != nil {
		return nil, fmt.Errorf("会话事件解码失败：%w", err)
	}
	return &ev, nil
}

func readFrame(r io.Reader, timeout time.Duration) ([]byte, error) {
	if timeout > 0 {
		if tr, ok := r.(interface{ SetReadDeadline(time.Time) error }); ok {
			_ = tr.SetReadDeadline(time.Now().Add(timeout))
			defer func() { _ = tr.SetReadDeadline(time.Time{}) }()
		}
	}
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(head[:])
	if n == 0 || n > MaxFrame {
		return nil, fmt.Errorf("控制帧长度非法：%d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
