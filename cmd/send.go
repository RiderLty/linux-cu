package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/linux-cu/pkg/usb"
)

// parseTarget parses a target specifier of the form "PROTO:address".
// Supported: "UDS:/path", "UDS:@name", "UDP:host:port", "UDP::port"
// Returns (protocol, address, error).
func parseTarget(target string) (string, string, error) {
	// Split on the first colon to get protocol
	idx := -1
	for i, c := range target {
		if c == ':' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", "", fmt.Errorf("无效目标格式 %q，应为 UDS:address 或 UDP:address", target)
	}
	proto := target[:idx]
	addr := target[idx+1:]

	switch proto {
	case "UDS", "uds":
		return "uds", addr, nil
	case "UDP", "udp":
		return "udp", addr, nil
	default:
		return "", "", fmt.Errorf("未知协议 %q，支持 UDS 或 UDP", proto)
	}
}

func runSend(device string, target string, debug bool) error {
	// Parse target
	proto, addr, err := parseTarget(target)
	if err != nil {
		return err
	}

	// Resolve USB device
	busNum, devAddr, err := resolveDevice(device)
	if err != nil {
		return err
	}

	log.Printf("[描述符] 读取设备 %d:%d ...", busNum, devAddr)
	configs, _, err := usb.ReadDescriptors(busNum, devAddr)
	if err != nil {
		return fmt.Errorf("读取描述符: %w", err)
	}

	// Open real USB device
	devHandle, hidEPs, err := openRealDevice(busNum, devAddr, configs)
	if err != nil {
		return fmt.Errorf("打开设备: %w", err)
	}
	defer devHandle.Close()
	log.Printf("[USB] 已打开真实设备，%d 个 IN 端点，%d 个 OUT 端点", len(hidEPs.IN), len(hidEPs.OUT))

	// Build iface -> hidEndpoint map for OUT direction
	outMap := make(map[uint8]hidEndpoint)
	for _, ep := range hidEPs.OUT {
		outMap[ep.InterfaceNumber] = ep
	}

	// Create network connection
	var conn net.Conn
	switch proto {
	case "uds":
		uaddr, err := net.ResolveUnixAddr("unixgram", addr)
		if err != nil {
			return fmt.Errorf("解析 UDS 地址: %w", err)
		}
		// Explicit local bind so server can reply (auto-bind yields nil addr in ReadFromUnix)
		localName := fmt.Sprintf("@linux_cu_send_%d", os.Getpid())
		localAddr, err := net.ResolveUnixAddr("unixgram", localName)
		if err != nil {
			return fmt.Errorf("解析本地 UDS 地址: %w", err)
		}
		c, err := net.DialUnix("unixgram", localAddr, uaddr)
		if err != nil {
			return fmt.Errorf("连接 UDS %s: %w", addr, err)
		}
		conn = c
		log.Printf("[Send] 已连接 UDS %s (local=%s)", addr, localName)
	case "udp":
		raddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return fmt.Errorf("解析 UDP 地址: %w", err)
		}
		c, err := net.DialUDP("udp", nil, raddr)
		if err != nil {
			return fmt.Errorf("连接 UDP %s: %w", addr, err)
		}
		conn = c
		log.Printf("[Send] 已连接 UDP %s", addr)
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[主] 收到退出信号，清理中...")
		cancel()
	}()

	// Goroutine A: Read USB IN endpoints -> build packet -> send to network
	for _, ep := range hidEPs.IN {
		go pollAndSend(ctx, devHandle, ep, conn, debug)
	}

	// Goroutine B: Read from network -> parse -> write to USB OUT endpoints
	go recvAndWrite(ctx, conn, devHandle, outMap, debug)

	log.Println("[主] 进入主循环，Ctrl+C 退出 (发送模式: USB 设备 ↔ IPC)")
	<-ctx.Done()
	log.Println("[主] 退出")
	return nil
}

// pollAndSend reads from a USB IN endpoint and sends the data to the network connection.
func pollAndSend(ctx context.Context, dev *usb.DeviceHandle, ep hidEndpoint, conn net.Conn, debug bool) {
	pktSize := int(ep.MaxPacketSize)
	if pktSize < 8 {
		pktSize = 8
	}
	if pktSize > 512 {
		pktSize = 512
	}
	epType := ep.Attributes & 0x03
	log.Printf("[Send] 轮询接口 %d 端点 0x%02X (type=%d) -> IPC", ep.InterfaceNumber, ep.EndpointAddress, epType)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var data []byte
		var err error

		switch epType {
		case 0x02:
			data, err = dev.BulkRead(ep.EndpointAddress, pktSize, 100)
		case 0x03:
			data, err = dev.InterruptRead(ep.EndpointAddress, pktSize, 100)
		default:
			log.Printf("[Send] 不支持的端点类型 %d ep=0x%02X，跳过", epType, ep.EndpointAddress)
			return
		}

		if err != nil {
			log.Printf("[Send] 读取错误 ep=0x%02X: %v", ep.EndpointAddress, err)
			return
		}
		if len(data) == 0 {
			continue
		}

		if debug {
			log.Printf("[DEBUG][USB→IPC] iface=%d ep=0x%02X len=%d data=%x", ep.InterfaceNumber, ep.EndpointAddress, len(data), data)
		}

		pkt := buildInjectPacket(ep.InterfaceNumber, data)
		if _, err := conn.Write(pkt); err != nil {
			log.Printf("[Send] IPC写入失败: %v", err)
			return
		}
	}
}

// recvAndWrite reads from the network connection, parses injection packets,
// and writes the data to the corresponding USB OUT endpoint.
func recvAndWrite(ctx context.Context, conn net.Conn, dev *usb.DeviceHandle, outMap map[uint8]hidEndpoint, debug bool) {
	buf := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set a read deadline so we can check for context cancellation
		if uc, ok := conn.(*net.UDPConn); ok {
			uc.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		} else if uc, ok := conn.(*net.UnixConn); ok {
			uc.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		}

		n, err := conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			log.Printf("[Send] IPC读取错误: %v", err)
			return
		}
		if n == 0 {
			continue
		}

		ifaceNum, data, err := parseInjectPacket(buf[:n])
		if err != nil {
			log.Printf("[Send] 解析数据包失败: %v", err)
			continue
		}

		outEP, ok := outMap[ifaceNum]
		if !ok {
			if debug {
				log.Printf("[Send] 无 OUT 端点对应接口 %d，丢弃", ifaceNum)
			}
			continue
		}

		epType := outEP.Attributes & 0x03
		if debug {
			log.Printf("[DEBUG][IPC→USB] iface=%d ep=0x%02X type=%d len=%d data=%x", ifaceNum, outEP.EndpointAddress, epType, len(data), data)
		}

		switch epType {
		case 0x02:
			if err := dev.BulkWrite(outEP.EndpointAddress, data, 1000); err != nil {
				log.Printf("[Send] 批量写入 ep=0x%02X 失败: %v", outEP.EndpointAddress, err)
			}
		case 0x03:
			if err := dev.InterruptWrite(outEP.EndpointAddress, data, 1000); err != nil {
				log.Printf("[Send] 中断写入 ep=0x%02X 失败: %v", outEP.EndpointAddress, err)
			}
		default:
			log.Printf("[Send] 不支持的端点类型 %d ep=0x%02X", epType, outEP.EndpointAddress)
		}
	}
}
