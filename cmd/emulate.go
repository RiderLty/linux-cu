package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/linux-cu/pkg/gadget"
	"github.com/linux-cu/pkg/pipe"
	"github.com/linux-cu/pkg/usb"
)

func parseVIDPID(vidHex, pidHex string) (uint16, uint16, error) {
	vid, err := strconv.ParseUint(vidHex, 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid VID: %w", err)
	}
	pid, err := strconv.ParseUint(pidHex, 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid PID: %w", err)
	}
	return uint16(vid), uint16(pid), nil
}

func usbListDevices() ([]usb.DeviceInfo, error)  { return usb.ListDevices() }
func usbPrintDeviceInfo(d []usb.DeviceInfo)       { usb.PrintDeviceInfo(d) }
func usbFindDevice(vid, pid uint16) (int, int, error) {
	return usb.FindDeviceByVIDPID(vid, pid)
}

func runEmulate(busNum, devAddr int, debug bool, udsAddr, udpAddr string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[主] 收到退出信号，清理中...")
		cancel()
	}()

	log.Printf("[描述符] 读取设备 %d:%d ...", busNum, devAddr)
	configs, devDesc, err := usb.ReadDescriptors(busNum, devAddr)
	if err != nil {
		return fmt.Errorf("读取描述符: %w", err)
	}
	usb.PrintFullDescriptors(devDesc, configs)

	// Use original device's configuration attributes
	var maxPower uint8 = 100
	var selfPowered, remoteWakeup bool
	if len(configs) > 0 {
		maxPower = configs[0].MaxPower
		selfPowered = configs[0].SelfPowered
		remoteWakeup = configs[0].RemoteWakeup
	}

	gadgetCfg := gadget.Config{
		VID:            devDesc.VendorID,
		PID:            devDesc.ProductID,
		Manufacturer:   devDesc.Manufacturer,
		Product:        devDesc.Product,
		SerialNumber:   "", // don't set empty string - it creates a spurious string descriptor
		DeviceClass:    devDesc.DeviceClass,
		DeviceSubClass: devDesc.DeviceSubClass,
		DeviceProtocol: devDesc.DeviceProtocol,
		MaxPacketSize0: devDesc.MaxPacketSize0,
		BcdDevice:      devDesc.BcdDevice,
		BcdUSB:         devDesc.BcdUSB,
		MaxPower:       maxPower,
		SelfPowered:    selfPowered,
		RemoteWakeup:   remoteWakeup,
	}

	log.Printf("[Gadget] 创建 VID=0x%04X PID=0x%04X", devDesc.VendorID, devDesc.ProductID)
	g, err := gadget.Create(gadgetCfg)
	if err != nil {
		return fmt.Errorf("创建 Gadget: %w", err)
	}
	defer g.Destroy()
	log.Printf("[Gadget] 已创建: %s", g.Name)

	// Add HID functions for each HID interface
	if len(configs) == 0 {
		return fmt.Errorf("设备无配置描述符")
	}
	cfg := configs[0]

	// Track which HID interfaces exist and their report lengths
	hidIdx := 0
	for _, iface := range cfg.Interfaces {
		if iface.InterfaceClass != 0x03 {
			if iface.AlternateSetting == 0 {
				log.Printf("[Gadget] 跳过非 HID 接口 %d (class=0x%02x subclass=0x%02x protocol=0x%02x)",
					iface.InterfaceNumber, iface.InterfaceClass, iface.InterfaceSubClass, iface.InterfaceProtocol)
			}
			continue
		}
		if iface.AlternateSetting != 0 {
			continue // only create function for alt setting 0
		}
		instance := fmt.Sprintf("usb%d", hidIdx)
		reportLen := uint16(64) // default
		if len(iface.Endpoints) > 0 {
			for _, ep := range iface.Endpoints {
				if ep.EndpointAddress&0x80 != 0 {
					reportLen = ep.MaxPacketSize
					break
				}
			}
		}
		hidCfg := gadget.HIDConfig{
			Instance:   instance,
			Protocol:   iface.InterfaceProtocol,
			SubClass:   iface.InterfaceSubClass,
			ReportLen:  reportLen,
			ReportDesc: iface.ReportDescriptor,
		}
		log.Printf("[Gadget] 添加 HID 功能 %s (iface=%d, protocol=%d, subclass=%d, report_len=%d, report_desc=%d bytes)",
			instance, iface.InterfaceNumber, hidCfg.Protocol, hidCfg.SubClass, hidCfg.ReportLen, len(hidCfg.ReportDesc))
		if err := g.AddHIDFunction(iface.InterfaceNumber, hidCfg); err != nil {
			return fmt.Errorf("添加 HID 功能: %w", err)
		}
		hidIdx++
	}

	if hidIdx == 0 {
		return fmt.Errorf("设备无 HID 接口 (所有接口均为非 HID 类，当前仅支持 HID 类设备透传)")
	}

	log.Println("[Gadget] 连接 UDC ...")
	if err := g.ConnectUDC(); err != nil {
		return fmt.Errorf("连接 UDC: %w", err)
	}
	log.Println("[Gadget] 已连接到主机")

	p, pipeCtx := pipe.NewWithContext(ctx, 64)
	defer p.Close()

	devHandle, hidEPs, err := openRealDevice(busNum, devAddr, configs)
	if err != nil {
		log.Printf("[USB] 打开真实设备失败: %v", err)
	} else {
		defer devHandle.Close()
		startHIDPolling(pipeCtx, devHandle, hidEPs, p, debug)
	}
	_ = hidEPs

	startGadgetIO(pipeCtx, g, p, busNum, devAddr, debug)

	// Start IPC injection if configured
	if udsAddr != "" {
		startIPCInjection(pipeCtx, "uds", udsAddr, p, g.IfaceToHidIdx, debug)
	}
	if udpAddr != "" {
		startIPCInjection(pipeCtx, "udp", udpAddr, p, g.IfaceToHidIdx, debug)
	}

	log.Println("[主] 进入主循环，Ctrl+C 退出")
	<-pipeCtx.Done()
	log.Println("[主] 退出")
	return nil
}
