package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/linux-cu/pkg/pipe"
)

type UDSPacketConn struct {
	conn *net.UnixConn
	ch   chan []byte
}

func (u *UDSPacketConn) ReadChan() <-chan []byte { return u.ch }

func (u *UDSPacketConn) Close() error {
	return u.conn.Close()
}

func (u *UDSPacketConn) serve() {
	buf := make([]byte, 65536)
	for {
		n, _, err := u.conn.ReadFromUnix(buf)
		if err != nil {
			close(u.ch)
			return
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
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

type UDPPacketConn struct {
	conn *net.UDPConn
	ch   chan []byte
}

func (u *UDPPacketConn) ReadChan() <-chan []byte { return u.ch }

func (u *UDPPacketConn) Close() error { return u.conn.Close() }

func (u *UDPPacketConn) serve() {
	buf := make([]byte, 65536)
	for {
		n, _, err := u.conn.ReadFromUDP(buf)
		if err != nil {
			close(u.ch)
			return
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
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

// ========== UDP Writer ==========

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

// ========== IPC Injection ==========

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
