package serial

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.bug.st/serial"
)

// Config 串口参数
type Config struct {
	Device string // 例如 "/dev/ttyACM0"
	Debug  bool   // true=115200, false=4000000
}

const DefaultBaudRate = 4000000

// Port 帧级别的串口读写
type Port struct {
	port    serial.Port
	encoder Encoder
	decoder Decoder
	writeCh chan Frame  // 出站帧队列
	readCh  chan Frame  // 入站帧队列
	rawCh   chan []byte // 非帧原始数据（bootloader 日志等）
	errCh   chan error  // 致命错误
}

const (
	writeChanSize = 32
	readChanSize  = 16
	rawChanSize   = 16
	errChanSize   = 4
	readBufSize   = 256
)

// Open 打开串口并启动读写 goroutine
func Open(ctx context.Context, cfg Config) (*Port, error) {
	baud := DefaultBaudRate
	if cfg.Debug {
		baud = 115200
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

	// 设置短读超时，以便 reader goroutine 能检查 ctx.Done()
	if err := sp.SetReadTimeout(100 * time.Millisecond); err != nil {
		sp.Close()
		return nil, fmt.Errorf("set read timeout: %w", err)
	}

	p := &Port{
		port:    sp,
		writeCh: make(chan Frame, writeChanSize),
		readCh:  make(chan Frame, readChanSize),
		rawCh:   make(chan []byte, rawChanSize),
		errCh:   make(chan error, errChanSize),
	}

	go p.reader(ctx)
	go p.writer(ctx)

	return p, nil
}

// SendFrame 入队一帧等待发送（非阻塞）
func (p *Port) SendFrame(f Frame) {
	p.writeCh <- f
}

// RecvChan 返回收到帧的 channel
func (p *Port) RecvChan() <-chan Frame {
	return p.readCh
}

// RawChan 返回非帧原始数据的 channel（bootloader 日志等）
func (p *Port) RawChan() <-chan []byte {
	return p.rawCh
}

// ErrChan 返回致命错误的 channel
func (p *Port) ErrChan() <-chan error {
	return p.errCh
}

// Close 关闭串口
func (p *Port) Close() error {
	return p.port.Close()
}

func (p *Port) reader(ctx context.Context) {
	buf := make([]byte, readBufSize)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := p.port.Read(buf)
		log.Printf("Read %d bytes", n)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				p.errCh <- fmt.Errorf("serial read: %w", err)
				return
			}
		}
		if n == 0 {
			continue // 读超时，循环检查 ctx
		}
		log.Printf("Received %d bytes", n)
		log.Printf("Received data: %x", buf[:n])
		frames, raw, err := p.decoder.Feed(buf[:n])
		if err != nil {
			// CRC 错误等，解码器已自动重置状态，继续处理
		}
		for _, f := range frames {
			select {
			case p.readCh <- f:
			case <-ctx.Done():
				return
			}
		}
		if len(raw) > 0 {
			select {
			case p.rawCh <- raw:
			default:
				// rawCh 满了，丢弃
			}
		}
	}
}

func (p *Port) writer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-p.writeCh:
			encoded := p.encoder.Encode(f.Cmd, f.Payload)
			_, err := p.port.Write(encoded)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					p.errCh <- fmt.Errorf("serial write: %w", err)
					return
				}
			}
		}
	}
}
