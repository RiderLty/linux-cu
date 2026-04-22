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
	Attributes    uint8 // bmAttributes (transfer type: 1=isoc, 2=bulk, 3=interrupt)
	MaxPacketSize uint16
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
			if iface.AlternateSetting != 0 {
				continue // only claim alt setting 0
			}
			_ = devHandle.DetachKernelDriver(iface.InterfaceNumber)
			if err := devHandle.ClaimInterface(iface.InterfaceNumber); err != nil {
				log.Printf("[USB] 声明接口 %d 失败: %v", iface.InterfaceNumber, err)
				continue
			}
			for _, ep := range iface.Endpoints {
				hidEp := hidEndpoint{
					InterfaceNumber: iface.InterfaceNumber,
					EndpointAddress: ep.EndpointAddress,
					Attributes:      ep.Attributes,
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
	log.Printf("[USB] 已打开真实设备，%d 个 IN 端点，%d 个 OUT 端点", len(eps.IN), len(eps.OUT))
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
	if pktSize > 512 {
		pktSize = 512
	}
	epType := ep.Attributes & 0x03
	log.Printf("[USB] 轮询接口 %d 端点 0x%02X (type=%d)", ep.InterfaceNumber, ep.EndpointAddress, epType)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var data []byte
		var err error

		switch epType {
		case 0x02: // Bulk
			data, err = dev.BulkRead(ep.EndpointAddress, pktSize, 100)
		case 0x03: // Interrupt
			data, err = dev.InterruptRead(ep.EndpointAddress, pktSize, 100)
		default:
			// Isochronous not supported via libusb sync API, skip
			log.Printf("[USB] 等时端点 0x%02X 不支持同步读取，跳过", ep.EndpointAddress)
			return
		}

		if err != nil {
			log.Printf("[USB] 读取错误 ep=0x%02X: %v", ep.EndpointAddress, err)
			return
		}
		if len(data) == 0 {
			continue
		}
		if debug {
			log.Printf("[DEBUG][USB→Pipe] iface=%d ep=0x%02X len=%d data=%x", ep.InterfaceNumber, ep.EndpointAddress, len(data), data)
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
			continue
		}

		epType := outEP.Attributes & 0x03
		if debug {
			log.Printf("[DEBUG][Pipe→USB] iface=%d ep=0x%02X type=%d len=%d data=%x", msg.Interface, outEP.EndpointAddress, epType, len(msg.Data), msg.Data)
		}

		switch epType {
		case 0x02: // Bulk
			if err := dev.BulkWrite(outEP.EndpointAddress, msg.Data, 1000); err != nil {
				log.Printf("[USB] 批量写入 ep=0x%02X 失败: %v", outEP.EndpointAddress, err)
			}
		case 0x03: // Interrupt
			if err := dev.InterruptWrite(outEP.EndpointAddress, msg.Data, 1000); err != nil {
				log.Printf("[USB] 中断写入 ep=0x%02X 失败: %v", outEP.EndpointAddress, err)
			}
		default:
			log.Printf("[USB] 不支持的端点类型 %d ep=0x%02X", epType, outEP.EndpointAddress)
		}
	}
}
