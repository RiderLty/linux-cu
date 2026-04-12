package protocol

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/linux-cu/client/pkg/serial"
)

// Session 管理请求/响应关联
type Session struct {
	port *serial.Port
	mu   sync.Mutex // 串行化 SendAndWait / Expect 操作
}

// NewSession 创建会话
func NewSession(port *serial.Port) *Session {
	return &Session{port: port}
}

// SendAndWait 发送帧并等待指定命令的响应，支持超时和重试
// 等待期间收到的不匹配帧会被丢弃（CTRL_REQ 等由 emulation loop 处理）
func (s *Session) SendAndWait(ctx context.Context, send serial.Frame, expectCmd byte, timeout time.Duration, retries int) (*serial.Frame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for attempt := 0; attempt <= retries; attempt++ {
		s.port.SendFrame(send)

		resp, err := s.waitFor(ctx, expectCmd, timeout)
		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// 超时，重试
	}
	return nil, fmt.Errorf("timeout waiting for CMD 0x%02X after %d retries", expectCmd, retries)
}

// SendFireAndForget 发送帧不等待响应
func (s *Session) SendFireAndForget(f serial.Frame) {
	s.port.SendFrame(f)
}

// Expect 等待指定命令的帧（不发送）
func (s *Session) Expect(ctx context.Context, cmd byte, timeout time.Duration) (*serial.Frame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.waitFor(ctx, cmd, timeout)
}

// waitFor 内部方法：从 RecvChan 读取直到获得目标 cmd 或超时
// 调用者必须持有 s.mu
func (s *Session) waitFor(ctx context.Context, cmd byte, timeout time.Duration) (*serial.Frame, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case f := <-s.port.RecvChan():
			if f.Cmd == cmd {
				return &f, nil
			} else {
				log.Printf("Discarding frame: cmd=0x%02X payload=%v", f.Cmd, f.Payload)
			}
			// 其他非目标帧，丢弃并继续等待

		case <-timer.C:
			return nil, fmt.Errorf("timeout waiting for CMD 0x%02X", cmd)

		case <-ctx.Done():
			return nil, ctx.Err()

		case err := <-s.port.ErrChan():
			return nil, err
		}
	}
}
