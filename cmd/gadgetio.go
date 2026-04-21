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

	// Writer: pipe DeviceToHost -> /dev/hidgN write
	// HID interfaces are added in order (0,1,2,...), so hidIdx = msg.Interface
	go func() {
		for {
			msg, err := p.RecvDeviceToHost(ctx)
			if err != nil {
				return
			}
			switch msg.Type {
			case pipe.MsgData:
				hidIdx := msg.Interface
				if int(hidIdx) >= len(hidFiles) || hidFiles[hidIdx] == nil {
					log.Printf("[Gadget] 无 hidg 设备对应接口 %d (共 %d 个)", hidIdx, len(hidFiles))
					continue
				}
				if debug {
					log.Printf("[DEBUG][Pipe→HIDG] iface=%d hidg%d len=%d data=%x", hidIdx, hidIdx, len(msg.Data), msg.Data)
				}
				if _, err := hidFiles[hidIdx].Write(msg.Data); err != nil {
					log.Printf("[Gadget] 写入 %s 失败: %v", hidFuncs[hidIdx].DevPath, err)
				}
			case pipe.MsgControl:
				handleGadgetCtrlReq(ctx, msg, busNum, devAddr, p)
			}
		}
	}()

	// Reader: /dev/hidgN read -> pipe HostToDevice (for OUT data from host)
	for i, f := range hidFiles {
		if f == nil {
			continue
		}
		go readHIDDevice(ctx, f, uint8(i), p, debug)
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
