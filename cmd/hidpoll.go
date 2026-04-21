package main

import (
	"context"
	"log"

	"github.com/linux-cu/pkg/pipe"
	"github.com/linux-cu/pkg/usb"
)

type hidEndpoint struct {
	InterfaceNumber uint8
	EndpointAddress uint8
	MaxPacketSize   uint16
}

type hidEndpoints struct {
	IN  []hidEndpoint
	OUT []hidEndpoint
}

func openRealDevice(busNum, devAddr int, configs []usb.ConfigDescriptor) (*usb.DeviceHandle, hidEndpoints, error) {
	devHandle, err := usb.OpenDevice(busNum, devAddr)
	if err != nil {
		return nil, hidEndpoints{}, err
	}

	var eps hidEndpoints
	if len(configs) > 0 {
		for _, iface := range configs[0].Interfaces {
			if iface.InterfaceClass != 0x03 {
				continue
			}
			_ = devHandle.DetachKernelDriver(iface.InterfaceNumber)
			if err := devHandle.ClaimInterface(iface.InterfaceNumber); err != nil {
				log.Printf("[HID] 声明接口 %d 失败: %v", iface.InterfaceNumber, err)
				continue
			}
			for _, ep := range iface.Endpoints {
				if (ep.Attributes & 0x03) != 0x03 {
					continue // only interrupt endpoints
				}
				hidEp := hidEndpoint{
					InterfaceNumber: iface.InterfaceNumber,
					EndpointAddress: ep.EndpointAddress,
					MaxPacketSize:   ep.MaxPacketSize,
				}
				if ep.EndpointAddress&0x80 != 0 {
					eps.IN = append(eps.IN, hidEp)
				} else {
					eps.OUT = append(eps.OUT, hidEp)
				}
			}
		}
	}
	log.Printf("[HID] 已打开真实设备，%d 个中断 IN 端点，%d 个中断 OUT 端点", len(eps.IN), len(eps.OUT))
	return devHandle, eps, nil
}

func startHIDPolling(ctx context.Context, dev *usb.DeviceHandle, eps hidEndpoints, p *pipe.Pipe, debug bool) {
	// IN: real device -> pipe
	for _, ep := range eps.IN {
		go pollEndpoint(ctx, dev, ep, p, debug)
	}

	// OUT: pipe -> real device
	if len(eps.OUT) > 0 {
		go forwardOutToRealDevice(ctx, dev, eps.OUT, p, debug)
	}
}

func pollEndpoint(ctx context.Context, dev *usb.DeviceHandle, ep hidEndpoint, p *pipe.Pipe, debug bool) {
	pktSize := int(ep.MaxPacketSize)
	if pktSize < 8 {
		pktSize = 8
	}
	if pktSize > 255 {
		pktSize = 255
	}
	log.Printf("[HID] 轮询接口 %d 端点 0x%02X", ep.InterfaceNumber, ep.EndpointAddress)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		data, err := dev.InterruptRead(ep.EndpointAddress, pktSize, 100)
		if err != nil {
			log.Printf("[HID] 读取错误: %v", err)
			return
		}
		if len(data) == 0 {
			continue
		}
		if debug {
			log.Printf("[DEBUG][HID→Pipe] iface=%d ep=0x%02X len=%d data=%x", ep.InterfaceNumber, ep.EndpointAddress, len(data), data)
		}
		msg := pipe.DataMsg(pipe.DeviceToHost, ep.EndpointAddress, ep.InterfaceNumber, data)
		if err := p.SendDeviceToHost(ctx, msg); err != nil {
			return
		}
	}
}

// forwardOutToRealDevice reads HostToDevice messages from the pipe
// and writes them to the real device's OUT endpoints.
func forwardOutToRealDevice(ctx context.Context, dev *usb.DeviceHandle, outEPs []hidEndpoint, p *pipe.Pipe, debug bool) {
	// Build a map: interface number -> OUT endpoint
	outMap := make(map[uint8]hidEndpoint)
	for _, ep := range outEPs {
		outMap[ep.InterfaceNumber] = ep
	}

	for {
		msg, err := p.RecvHostToDevice(ctx)
		if err != nil {
			return
		}
		if msg.Type != pipe.MsgData {
			continue
		}

		outEP, ok := outMap[msg.Interface]
		if !ok {
			// No OUT endpoint for this interface - data is dropped
			continue
		}

		if debug {
			log.Printf("[DEBUG][Pipe→HID] iface=%d ep=0x%02X len=%d data=%x", msg.Interface, outEP.EndpointAddress, len(msg.Data), msg.Data)
		}
		if err := dev.InterruptWrite(outEP.EndpointAddress, msg.Data, 1000); err != nil {
			log.Printf("[HID] 写入真实设备 ep=0x%02X 失败: %v", outEP.EndpointAddress, err)
		}
	}
}
