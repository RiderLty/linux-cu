package session

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/linux-cu/client/pkg/protocol"
	"github.com/linux-cu/client/pkg/serial"
	"github.com/linux-cu/client/pkg/usb"
)

// Config 会话配置
type Config struct {
	SerialDevice string
	Debug        bool
	BusNumber    int
	DevAddress   int
}

// Session 管理完整仿真生命周期
type Session struct {
	cfg   Config
	port  *serial.Port
	proto *protocol.Session
}

// New 创建会话
func New(cfg Config) *Session {
	return &Session{cfg: cfg}
}

// Run 执行完整仿真流程
func (s *Session) Run(ctx context.Context) error {
	port, err := serial.Open(ctx, serial.Config{
		Device: s.cfg.SerialDevice,
		Debug:  s.cfg.Debug,
	})
	if err != nil {
		return fmt.Errorf("open serial: %w", err)
	}
	defer port.Close()
	s.port = port
	s.proto = protocol.NewSession(port)

	// Phase 1: 握手
	if err := s.handshake(ctx); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}

	// Phase 2: 读取真实设备描述符
	log.Printf("[描述符] 读取设备 %d:%d 的描述符...", s.cfg.BusNumber, s.cfg.DevAddress)
	configs, devDesc, err := usb.ReadDescriptors(s.cfg.BusNumber, s.cfg.DevAddress)
	if err != nil {
		return fmt.Errorf("read descriptors: %w", err)
	}
	usb.PrintFullDescriptors(devDesc, configs)

	// Phase 3: 绑定设备
	if err := s.bindDevice(ctx, devDesc.VendorID, devDesc.ProductID); err != nil {
		return fmt.Errorf("bind device: %w", err)
	}

	// Phase 4: 发送描述符
	if err := s.sendDescriptors(ctx, devDesc, configs); err != nil {
		return fmt.Errorf("send descriptors: %w", err)
	}

	// Phase 5: 启动仿真
	if err := s.startEmulation(ctx); err != nil {
		return fmt.Errorf("start emulation: %w", err)
	}

	// Phase 6: 仿真循环 — 转发控制传输 + 心跳
	return s.emulationLoop(ctx)
}

// handshake 发送 PING，等待 PONG + "P4OK"
func (s *Session) handshake(ctx context.Context) error {
	log.Println("[握手] 发送 PING...")
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := s.proto.SendAndWait(ctx,
			serial.Frame{Cmd: protocol.CmdPing},
			protocol.CmdPong,
			time.Duration(protocol.PingTimeoutMs)*time.Millisecond,
			0,
		)
		if err != nil {
			log.Printf("[握手] 超时，重试 (%d/3): %v", attempt+1, err)
			continue
		}
		magic := string(resp.Payload)
		if magic != protocol.HandshakeMagic {
			return fmt.Errorf("unexpected PONG payload: %q", magic)
		}
		log.Println("[握手] 成功，P4 就绪")
		return nil
	}
	return fmt.Errorf("handshake failed after 3 attempts")
}

// bindDevice 发送 BIND_DEVICE
func (s *Session) bindDevice(ctx context.Context, vid, pid uint16) error {
	log.Printf("[绑定] VID=0x%04X PID=0x%04X", vid, pid)
	payload := (&protocol.BindDeviceReq{VID: vid, PID: pid}).Marshal()
	_, err := s.proto.SendAndWait(ctx,
		serial.Frame{Cmd: protocol.CmdBindDevice, Payload: payload},
		protocol.CmdBindAck,
		time.Duration(protocol.PingTimeoutMs)*time.Millisecond,
		protocol.PingRetries,
	)
	if err != nil {
		return err
	}
	log.Println("[绑定] 成功")
	return nil
}

// sendDescriptors 将设备描述符、配置描述符、HID报告描述符分块发送给 P4
func (s *Session) sendDescriptors(ctx context.Context, devDesc usb.DeviceDescriptor, configs []usb.ConfigDescriptor) error {
	// 1. 设备描述符
	if err := s.sendOneDescriptor(ctx, protocol.DescIDDevice, devDesc.Raw); err != nil {
		return fmt.Errorf("device desc: %w", err)
	}
	log.Println("[描述符] 设备描述符已发送")

	// 2. 配置描述符 (含所有接口和端点)
	if err := s.sendOneDescriptor(ctx, protocol.DescIDConfig, configs[0].Raw); err != nil {
		return fmt.Errorf("config desc: %w", err)
	}
	log.Println("[描述符] 配置描述符已发送")

	// 3. HID 报告描述符 (每个 HID 接口一个)
	for _, iface := range configs[0].Interfaces {
		if iface.InterfaceClass == 0x03 && len(iface.ReportDescriptor) > 0 {
			descID := protocol.DescIDReportBase + iface.InterfaceNumber
			if err := s.sendOneDescriptor(ctx, descID, iface.ReportDescriptor); err != nil {
				return fmt.Errorf("report desc iface %d: %w", iface.InterfaceNumber, err)
			}
			log.Printf("[描述符] HID 接口 %d 报告描述符已发送 (%d bytes)", iface.InterfaceNumber, len(iface.ReportDescriptor))
		}
	}

	log.Println("[描述符] 全部发送完成")
	return nil
}

// sendOneDescriptor 将单个描述符分块通过 DESC_BUNDLE 发送
func (s *Session) sendOneDescriptor(ctx context.Context, descID byte, data []byte) error {
	chunkDataMax := protocol.MaxDescChunk
	total := (len(data) + chunkDataMax - 1) / chunkDataMax
	if total > 255 {
		return fmt.Errorf("descriptor too large: %d bytes", len(data))
	}
	chunkTotal := byte(total)

	for i := 0; i < total; i++ {
		offset := i * chunkDataMax
		end := offset + chunkDataMax
		if end > len(data) {
			end = len(data)
		}
		chunk := data[offset:end]

		bundle := protocol.DescBundle{
			DescID:     descID,
			ChunkIdx:   byte(i),
			ChunkTotal: chunkTotal,
			Data:       chunk,
		}

		_, err := s.proto.SendAndWait(ctx,
			serial.Frame{Cmd: protocol.CmdDescBundle, Payload: bundle.Marshal()},
			protocol.CmdDescAck,
			time.Duration(protocol.DescAckTimeoutMs)*time.Millisecond,
			protocol.DescRetries,
		)
		if err != nil {
			return fmt.Errorf("chunk %d/%d: %w", i, total, err)
		}
	}
	return nil
}

// startEmulation 发送 START_EMUL，等待 EMUL_ACK
func (s *Session) startEmulation(ctx context.Context) error {
	log.Println("[仿真] 发送 START_EMUL...")
	_, err := s.proto.SendAndWait(ctx,
		serial.Frame{Cmd: protocol.CmdStartEmul},
		protocol.CmdEmulAck,
		time.Duration(protocol.StartEmulTimeoutMs)*time.Millisecond,
		protocol.StartEmulRetries,
	)
	if err != nil {
		return err
	}
	log.Println("[仿真] 已启动")
	return nil
}

// emulationLoop 仿真主循环：处理控制传输 + 心跳
// 仿真期间所有帧收发在此统一处理，不使用 protocol.Session.SendAndWait
func (s *Session) emulationLoop(ctx context.Context) error {
	log.Println("[仿真] 进入仿真循环，Ctrl+C 停止")

	heartbeatTicker := time.NewTicker(time.Duration(protocol.HeartbeatIntervalS) * time.Second)
	defer heartbeatTicker.Stop()
	missCount := 0

	// 心跳等待状态
	var hbMu sync.Mutex
	hbPending := false
	hbCh := make(chan struct{}, 1)

	for {
		select {
		case <-ctx.Done():
			log.Println("[仿真] 停止仿真...")
			s.port.SendFrame(serial.Frame{Cmd: protocol.CmdStopEmul})
			return ctx.Err()

		case <-heartbeatTicker.C:
			hbMu.Lock()
			if hbPending {
				// 上一次心跳还没回来
				missCount++
				log.Printf("[心跳] 丢失 (%d/%d)", missCount, protocol.HeartbeatMissMax)
				if missCount >= protocol.HeartbeatMissMax {
					hbMu.Unlock()
					return fmt.Errorf("heartbeat lost %d times", missCount)
				}
			}
			hbPending = true
			hbMu.Unlock()
			s.port.SendFrame(serial.Frame{Cmd: protocol.CmdPing})

		case f := <-s.port.RecvChan():
			switch f.Cmd {
			case protocol.CmdPong:
				hbMu.Lock()
				if hbPending {
					hbPending = false
					missCount = 0
					_ = hbCh
				}
				hbMu.Unlock()

			case protocol.CmdCtrlReq:
				s.handleCtrlReq(&f)

			case protocol.CmdError:
				ep, err := protocol.UnmarshalErrorPayload(f.Payload)
				if err == nil {
					log.Printf("[错误] 来源=0x%02X 码=0x%02X %s", ep.OriginCmd, ep.ErrorCode, ep.ErrorMsg)
				}

			default:
				// 忽略其他帧
			}

		case err := <-s.port.ErrChan():
			return fmt.Errorf("serial error: %w", err)
		}
	}
}

// handleCtrlReq 处理 P4 转发的 USB 控制传输请求
func (s *Session) handleCtrlReq(f *serial.Frame) {
	req, err := protocol.UnmarshalCtrlReq(f.Payload)
	if err != nil {
		log.Printf("[控制传输] 解析错误: %v", err)
		return
	}

	log.Printf("[控制传输] bmRequestType=0x%02X bRequest=0x%02X wValue=0x%04X wIndex=0x%04X wLength=%d",
		req.BMRequestType, req.BRequest, req.WValue, req.WIndex, req.WLength)

	result, err := usb.ProxyCtrlTransfer(
		s.cfg.BusNumber, s.cfg.DevAddress,
		req.BMRequestType, req.BRequest,
		req.WValue, req.WIndex, req.WLength,
		req.DataPhase,
	)

	if err != nil {
		log.Printf("[控制传输] 执行失败: %v，STALL", err)
		s.port.SendFrame(serial.Frame{Cmd: protocol.CmdCtrlStall})
		return
	}

	if result.Stall {
		s.port.SendFrame(serial.Frame{Cmd: protocol.CmdCtrlStall})
		return
	}

	ctrlData := &protocol.CtrlData{Data: result.Data}
	s.port.SendFrame(serial.Frame{
		Cmd:     protocol.CmdCtrlData,
		Payload: ctrlData.Marshal(),
	})
}
