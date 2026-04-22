package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"

	"github.com/linux-cu/pkg/pipe"
)

// ========== UDS ==========

type UDSPacketConn struct {
	conn     *net.UnixConn
	ch       chan []byte
	lastAddr *net.UnixAddr
	mu       sync.Mutex
}

func (u *UDSPacketConn) ReadChan() <-chan []byte { return u.ch }

func (u *UDSPacketConn) Close() error {
	return u.conn.Close()
}

func (u *UDSPacketConn) LastAddr() *net.UnixAddr {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastAddr
}

func (u *UDSPacketConn) serve() {
	buf := make([]byte, 65536)
	for {
		n, addr, err := u.conn.ReadFromUnix(buf)
		if err != nil {
			close(u.ch)
			return
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		u.mu.Lock()
		u.lastAddr = addr
		u.mu.Unlock()
		u.ch <- pkt
	}
}

func CreateUDSReader(address string) (*UDSPacketConn, error) {
	addr, err := net.ResolveUnixAddr("unixgram", address)
	if err != nil {
		return nil, err
	}
	if address[0] != '@' {
		_ = os.Remove(address)
	}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		return nil, err
	}
	u := &UDSPacketConn{
		conn: conn,
		ch:   make(chan []byte, 16),
	}
	go u.serve()
	return u, nil
}

type UDSWriter struct {
	conn *net.UnixConn
}

func (w *UDSWriter) Write(p []byte) (int, error) {
	return w.conn.Write(p)
}

func (w *UDSWriter) Close() error { return w.conn.Close() }

func CreateUDSWriter(address string) (*UDSWriter, error) {
	addr, err := net.ResolveUnixAddr("unixgram", address)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return nil, err
	}
	return &UDSWriter{conn: conn}, nil
}

// ========== UDP ==========

type UDPPacketConn struct {
	conn     *net.UDPConn
	ch       chan []byte
	lastAddr *net.UDPAddr
	mu       sync.Mutex
}

func (u *UDPPacketConn) ReadChan() <-chan []byte { return u.ch }

func (u *UDPPacketConn) Close() error {
	return u.conn.Close()
}

func (u *UDPPacketConn) LastAddr() *net.UDPAddr {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastAddr
}

func (u *UDPPacketConn) serve() {
	buf := make([]byte, 65536)
	for {
		n, addr, err := u.conn.ReadFromUDP(buf)
		if err != nil {
			close(u.ch)
			return
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		u.mu.Lock()
		u.lastAddr = addr
		u.mu.Unlock()
		u.ch <- pkt
	}
}

func CreateUDPReader(address string) (*UDPPacketConn, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	u := &UDPPacketConn{
		conn: conn,
		ch:   make(chan []byte, 16),
	}
	go u.serve()
	return u, nil
}

type UDPWriter struct {
	conn *net.UDPConn
}

func (w *UDPWriter) Write(p []byte) (int, error) {
	return w.conn.Write(p)
}

func (w *UDPWriter) Close() error { return w.conn.Close() }

func CreateUDPWriter(remoteAddr string) (*UDPWriter, error) {
	raddr, err := net.ResolveUDPAddr("udp", remoteAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, err
	}
	return &UDPWriter{conn: conn}, nil
}

// ========== IPC Packet Format ==========

// HID injection packet format:
//   Offset  Size  Field
//   0       1     Magic (0xC0)
//   1       1     Interface Number (USB interface number, e.g. 0, 1, 3)
//   2       2     Data Length (big-endian, N)
//   4       N     HID Report Data

const hidInjectMagic byte = 0xC0

// parseInjectPacket parses a HID injection packet.
// Returns the interface number and HID report data, or an error.
func parseInjectPacket(pkt []byte) (ifaceNum uint8, data []byte, err error) {
	if len(pkt) < 4 {
		return 0, nil, fmt.Errorf("packet too short (%d bytes, minimum 4)", len(pkt))
	}
	if pkt[0] != hidInjectMagic {
		return 0, nil, fmt.Errorf("invalid magic 0x%02x, expected 0x%02x", pkt[0], hidInjectMagic)
	}
	ifaceNum = pkt[1]
	dataLen := int(binary.BigEndian.Uint16(pkt[2:4]))
	if len(pkt) < 4+dataLen {
		return 0, nil, fmt.Errorf("packet truncated (declared %d bytes, got %d)", dataLen, len(pkt)-4)
	}
	data = make([]byte, dataLen)
	copy(data, pkt[4:4+dataLen])
	return ifaceNum, data, nil
}

// buildInjectPacket constructs a HID injection packet from interface number and data.
func buildInjectPacket(ifaceNum uint8, data []byte) []byte {
	pkt := make([]byte, 4+len(data))
	pkt[0] = hidInjectMagic
	pkt[1] = ifaceNum
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(data)))
	copy(pkt[4:], data)
	return pkt
}

// ========== IPC target address helpers ==========

// ipcTarget tracks the last received source address across UDS and UDP connections.
// Each protocol's connection tracks its own lastAddr; this merges them into a single target.
type ipcTarget struct {
	mu   sync.Mutex
	conn net.PacketConn
	addr net.Addr
}

func (t *ipcTarget) update(conn net.PacketConn, addr net.Addr) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.conn = conn
	t.addr = addr
}

func (t *ipcTarget) send(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil || t.addr == nil {
		return nil
	}
	_, err := t.conn.WriteTo(data, t.addr)
	return err
}

// ========== IPC Injection (unidirectional, existing) ==========

// startIPCInjection starts a UDS or UDP listener that receives HID injection packets
// and sends them into the pipe as DeviceToHost messages.
func startIPCInjection(ctx context.Context, proto, addr string, p *pipe.Pipe, ifaceMap map[uint8]int, debug bool) {
	var ch <-chan []byte
	var closer io.Closer

	switch proto {
	case "uds":
		conn, err := CreateUDSReader(addr)
		if err != nil {
			log.Printf("[IPC] UDS 监听 %s 失败: %v", addr, err)
			return
		}
		ch = conn.ReadChan()
		closer = conn
		log.Printf("[IPC] UDS 监听 %s 已开启", addr)
	case "udp":
		conn, err := CreateUDPReader(addr)
		if err != nil {
			log.Printf("[IPC] UDP 监听 %s 失败: %v", addr, err)
			return
		}
		ch = conn.ReadChan()
		closer = conn
		log.Printf("[IPC] UDP 监听 %s 已开启", addr)
	default:
		log.Printf("[IPC] 未知协议 %s", proto)
		return
	}

	go func() {
		<-ctx.Done()
		closer.Close()
	}()

	go func() {
		for pkt := range ch {
			ifaceNum, data, err := parseInjectPacket(pkt)
			if err != nil {
				log.Printf("[IPC] 解析数据包失败: %v", err)
				continue
			}

			hidIdx, ok := ifaceMap[ifaceNum]
			if !ok {
				log.Printf("[IPC] 无 HID 功能对应接口 %d (映射=%v)", ifaceNum, ifaceMap)
				continue
			}

			if debug {
				log.Printf("[DEBUG][IPC→Pipe] iface=%d hidIdx=%d len=%d data=%x", ifaceNum, hidIdx, len(data), data)
			}

			msg := pipe.DataMsg(pipe.DeviceToHost, 0, ifaceNum, data)
			if err := p.SendDeviceToHost(ctx, msg); err != nil {
				return
			}
		}
	}()
}

// ========== IPC Bidirectional (--recv mode) ==========

// startIPCBidirectional starts a UDS or UDP listener that:
// 1. Receives HID injection packets and sends them into the pipe as DeviceToHost messages
// 2. Updates the shared target with the most recent source address
//
// A separate goroutine (started by the caller) reads HostToDevice messages from the pipe
// and sends them back to the shared target.
func startIPCBidirectional(ctx context.Context, proto, addr string, p *pipe.Pipe, ifaceMap map[uint8]int, target *ipcTarget, debug bool) {
	var ch <-chan []byte
	var closer io.Closer
	var pc net.PacketConn

	switch proto {
	case "uds":
		conn, err := CreateUDSReader(addr)
		if err != nil {
			log.Printf("[IPC-recv] UDS 监听 %s 失败: %v", addr, err)
			return
		}
		ch = conn.ReadChan()
		closer = conn
		pc = conn.conn
		log.Printf("[IPC-recv] UDS 双向监听 %s 已开启", addr)
	case "udp":
		conn, err := CreateUDPReader(addr)
		if err != nil {
			log.Printf("[IPC-recv] UDP 监听 %s 失败: %v", addr, err)
			return
		}
		ch = conn.ReadChan()
		closer = conn
		pc = conn.conn
		log.Printf("[IPC-recv] UDP 双向监听 %s 已开启", addr)
	default:
		log.Printf("[IPC-recv] 未知协议 %s", proto)
		return
	}

	go func() {
		<-ctx.Done()
		closer.Close()
	}()

	// Receive: IPC -> pipe, update target on each packet
	go func() {
		for pkt := range ch {
			// Update target using the lastAddr already tracked by serve()
			switch c := closer.(type) {
			case *UDSPacketConn:
				if a := c.LastAddr(); a != nil {
					target.update(pc, a)
				}
			case *UDPPacketConn:
				if a := c.LastAddr(); a != nil {
					target.update(pc, a)
				}
			}

			ifaceNum, data, err := parseInjectPacket(pkt)
			if err != nil {
				log.Printf("[IPC-recv] 解析数据包失败: %v", err)
				continue
			}

			hidIdx, ok := ifaceMap[ifaceNum]
			if !ok {
				log.Printf("[IPC-recv] 无 HID 功能对应接口 %d (映射=%v)", ifaceNum, ifaceMap)
				continue
			}

			if debug {
				log.Printf("[DEBUG][IPC→Pipe] iface=%d hidIdx=%d len=%d data=%x", ifaceNum, hidIdx, len(data), data)
			}

			msg := pipe.DataMsg(pipe.DeviceToHost, 0, ifaceNum, data)
			if err := p.SendDeviceToHost(ctx, msg); err != nil {
				return
			}
		}
	}()
}

// startIPCEcho starts a goroutine that reads HostToDevice messages from the pipe
// and sends them back via the shared target.
func startIPCEcho(ctx context.Context, p *pipe.Pipe, target *ipcTarget, debug bool) {
	go func() {
		for {
			msg, err := p.RecvHostToDevice(ctx)
			if err != nil {
				return
			}
			if msg.Type != pipe.MsgData {
				continue
			}

			pkt := buildInjectPacket(msg.Interface, msg.Data)
			if debug {
				log.Printf("[DEBUG][Pipe→IPC] iface=%d len=%d data=%x", msg.Interface, len(msg.Data), msg.Data)
			}
			if err := target.send(pkt); err != nil {
				log.Printf("[IPC-recv] 回传数据失败: %v", err)
			}
		}
	}()
}
