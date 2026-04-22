package main

import (
	"context"
	"log"
	"os"

	"github.com/linux-cu/pkg/gadget"
	"github.com/linux-cu/pkg/pipe"
	"github.com/linux-cu/pkg/usb"
)

func startGadgetIO(ctx context.Context, g *gadget.Gadget, p *pipe.Pipe, busNum, devAddr int, debug bool) {
	hidFuncs := g.HIDFunctions()
	hidFiles := make([]*os.File, len(hidFuncs))

	// Open /dev/hidgN devices
	for i, hf := range hidFuncs {
		f, err := hf.OpenDev()
		if err != nil {
			log.Printf("[Gadget] 打开 %s 失败: %v", hf.DevPath, err)
			continue
		}
		hidFiles[i] = f
		log.Printf("[Gadget] 已打开 %s (接口 %d -> hidg%d)", hf.DevPath, i, hf.Index)
	}

	go func() {
		<-ctx.Done()
		for _, f := range hidFiles {
			if f != nil {
				f.Close()
			}
		}
	}()

	// Writer: pipe DeviceToHost -> /dev/hidgN write or FFS
	// Use IfaceToHidIdx to translate original interface number to HID function index
	ifaceMap := g.IfaceToHidIdx
	ffsMap := g.IfaceToFFSIdx
	go func() {
		for {
			msg, err := p.RecvDeviceToHost(ctx)
			if err != nil {
				return
			}
			switch msg.Type {
			case pipe.MsgData:
				// Try HID first
				if hidIdx, ok := ifaceMap[msg.Interface]; ok && hidIdx < len(hidFiles) && hidFiles[hidIdx] != nil {
					if debug {
						log.Printf("[DEBUG][Pipe→HIDG] iface=%d hidg%d len=%d data=%x", msg.Interface, hidIdx, len(msg.Data), msg.Data)
					}
					if _, err := hidFiles[hidIdx].Write(msg.Data); err != nil {
						log.Printf("[Gadget] 写入 %s 失败: %v", hidFuncs[hidIdx].DevPath, err)
					}
					continue
				}
				// Try FFS - data is forwarded via the pipe to writeFFSEndpoints
				if _, ok := ffsMap[msg.Interface]; ok {
					// FFS OUT data is handled by writeFFSEndpoints reading from pipe
					// This message came from real device -> host, so it goes to FFS IN endpoint
					// which is handled by the FFS I/O goroutines
					if debug {
						log.Printf("[DEBUG][Pipe→FFS] iface=%d len=%d data=%x", msg.Interface, len(msg.Data), msg.Data)
					}
					continue
				}
				log.Printf("[Gadget] 无设备对应接口 %d (HID映射=%v, FFS映射=%v)", msg.Interface, ifaceMap, ffsMap)
			case pipe.MsgControl:
				handleGadgetCtrlReq(ctx, msg, busNum, devAddr, p)
			}
		}
	}()

	// Reader: /dev/hidgN read -> pipe HostToDevice (for OUT data from host)
	// Build reverse map: hidIdx -> original InterfaceNumber
	hidIdxToIface := make(map[int]uint8)
	for ifaceNum, idx := range ifaceMap {
		hidIdxToIface[idx] = ifaceNum
	}
	for i, f := range hidFiles {
		if f == nil {
			continue
		}
		ifaceNum := hidIdxToIface[i] // 0 if not found (acceptable default)
		go readHIDDevice(ctx, f, ifaceNum, p, debug)
	}
}

func handleGadgetCtrlReq(ctx context.Context, msg pipe.PipeMsg, busNum, devAddr int, p *pipe.Pipe) {
	result, err := usb.ProxyCtrlTransfer(
		busNum, devAddr,
		msg.BMRequestType, msg.BRequest,
		msg.WValue, msg.WIndex, msg.WLength,
		msg.Data,
	)
	if err != nil {
		resp := pipe.CtrlRespMsg(nil, true)
		p.SendHostToDevice(ctx, resp)
		return
	}
	if result.Stall {
		resp := pipe.CtrlRespMsg(nil, true)
		p.SendHostToDevice(ctx, resp)
		return
	}
	resp := pipe.CtrlRespMsg(result.Data, false)
	p.SendHostToDevice(ctx, resp)
}

func readHIDDevice(ctx context.Context, f *os.File, ifaceNum uint8, p *pipe.Pipe, debug bool) {
	buf := make([]byte, 1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := f.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			if debug {
				log.Printf("[DEBUG][HIDG→Pipe] iface=%d len=%d data=%x", ifaceNum, n, data)
			}
			msg := pipe.DataMsg(pipe.HostToDevice, 0, ifaceNum, data)
			p.SendHostToDevice(ctx, msg)
		}
	}
}
