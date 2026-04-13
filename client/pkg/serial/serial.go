package serial

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"go.bug.st/serial"
)

const (
	DefaultBaudRate = 4000000
	DebugBaudRate   = 115200
)

type Config struct {
	Device string
	Debug  bool
}

// Port 封装串口设备，提供帧级别的收发
type Port struct {
	port    serial.Port
	enc     Encoder
	dec     Decoder
	recvCh  chan Frame
	rawCh   chan []byte
	errCh   chan error
	closeCh chan struct{}
	wg      sync.WaitGroup
}

func Open(ctx context.Context, cfg Config) (*Port, error) {
	baud := DefaultBaudRate
	if cfg.Debug {
		baud = DebugBaudRate
	}

	mode := &serial.Mode{
		BaudRate: baud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	sp, err := serial.Open(cfg.Device, mode)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", cfg.Device, err)
	}

	// DTR/RTS 复位序列：触发 ESP32 重启
	// DTR 低 + RTS 高 → 拉低 EN → 释放 EN → ESP32 进入 bootloader 或正常启动
	if err := sp.SetDTR(false); err != nil {
		log.Printf("[串口] SetDTR(false): %v", err)
	}
	if err := sp.SetRTS(true); err != nil {
		log.Printf("[串口] SetRTS(true): %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := sp.SetDTR(true); err != nil {
		log.Printf("[串口] SetDTR(true): %v", err)
	}
	if err := sp.SetRTS(false); err != nil {
		log.Printf("[串口] SetRTS(false): %v", err)
	}
	log.Printf("[串口] DTR/RTS 复位序列完成")
	time.Sleep(500 * time.Millisecond) // 等待 ESP32 启动
	log.Printf("[串口] 等待 ESP32 启动完成")
	// 设置短读超时，以便 reader goroutine 能检查 ctx.Done()
	if err := sp.SetReadTimeout(100 * time.Millisecond); err != nil {
		sp.Close()
		return nil, fmt.Errorf("set read timeout: %w", err)
	}
	// 清空缓冲区
	_ = sp.ResetInputBuffer()
	_ = sp.ResetOutputBuffer()
	p := &Port{
		port:    sp,
		recvCh:  make(chan Frame, 32),
		rawCh:   make(chan []byte, 32),
		errCh:   make(chan error, 1),
		closeCh: make(chan struct{}),
	}
	p.wg.Add(2)
	go p.reader(ctx)
	go p.writer(ctx)
	log.Printf("[串口] 已打开 %s (%d baud)", cfg.Device, baud)
	return p, nil
}

func (p *Port) Close() {
	close(p.closeCh)
	p.port.Close()
	p.wg.Wait()
}

// RecvChan 返回接收到的帧
func (p *Port) RecvChan() <-chan Frame {
	return p.recvCh
}

// RawChan 返回非帧原始数据
func (p *Port) RawChan() <-chan []byte {
	return p.rawCh
}

// ErrChan 返回错误
func (p *Port) ErrChan() <-chan error {
	return p.errCh
}

// SendFrame 发送帧
func (p *Port) SendFrame(f Frame) {
	data := p.enc.Encode(f.Cmd, f.Payload)
	_, err := p.port.Write(data)
	if err != nil {
		log.Printf("[串口] 写入错误: %v", err)
	}
}

func (p *Port) reader(ctx context.Context) {
	defer p.wg.Done()
	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.closeCh:
			return
		default:
		}

		n, err := p.port.Read(buf)
		if err != nil {
			select {
			case p.errCh <- fmt.Errorf("read: %w", err):
			default:
			}
			return
		}

		if n > 0 {
			frames, raw, err := p.dec.Feed(buf[:n])
			if err != nil {
				log.Printf("[串口] 解码错误: %v", err)
				continue
			}
			for _, f := range frames {
				select {
				case p.recvCh <- f:
				default:
					log.Printf("[串口] 接收队列满，丢弃帧 cmd=0x%02X", f.Cmd)
				}
			}
			if len(raw) > 0 {
				select {
				case p.rawCh <- raw:
				default:
				}
			}
		}
	}
}

func (p *Port) writer(ctx context.Context) {
	defer p.wg.Done()
	<-ctx.Done()
}
