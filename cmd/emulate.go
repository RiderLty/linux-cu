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
	var configString string
	if len(configs) > 0 {
		maxPower = configs[0].MaxPower
		selfPowered = configs[0].SelfPowered
		remoteWakeup = configs[0].RemoteWakeup
		configString = configs[0].ConfigString
	}

	gadgetCfg := gadget.Config{
		VID:            devDesc.VendorID,
		PID:            devDesc.ProductID,
		Manufacturer:   devDesc.Manufacturer,
		Product:        devDesc.Product,
		SerialNumber:   devDesc.SerialNumber,
		DeviceClass:    devDesc.DeviceClass,
		DeviceSubClass: devDesc.DeviceSubClass,
		DeviceProtocol: devDesc.DeviceProtocol,
		MaxPacketSize0: devDesc.MaxPacketSize0,
		BcdDevice:      devDesc.BcdDevice,
		BcdUSB:         devDesc.BcdUSB,
		MaxPower:       maxPower,
		SelfPowered:    selfPowered,
		RemoteWakeup:   remoteWakeup,
		ConfigString:   configString,
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
	var nonHIDIfaceNums []uint8
	for _, iface := range cfg.Interfaces {
		if iface.InterfaceClass != 0x03 {
			if iface.AlternateSetting == 0 {
				log.Printf("[Gadget] 非 HID 接口 %d (class=0x%02x subclass=0x%02x protocol=0x%02x) -> FFS",
					iface.InterfaceNumber, iface.InterfaceClass, iface.InterfaceSubClass, iface.InterfaceProtocol)
				nonHIDIfaceNums = append(nonHIDIfaceNums, iface.InterfaceNumber)
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

	// Add FFS function for non-HID interfaces
	if len(nonHIDIfaceNums) > 0 {
		ffsName := "ffs.usb0"
		log.Printf("[Gadget] 添加 FFS 功能 %s (非 HID 接口: %v)", ffsName, nonHIDIfaceNums)
		ffsFunc, err := g.AddFFSFunction(ffsName, nonHIDIfaceNums)
		if err != nil {
			return fmt.Errorf("添加 FFS 功能: %w", err)
		}
		_ = ffsFunc // will be used after UDC connect
	}

	if hidIdx == 0 && len(nonHIDIfaceNums) == 0 {
		return fmt.Errorf("设备无可用的接口")
	}

	// Setup FFS (mount + write descriptors) BEFORE connecting UDC.
	// The kernel requires FFS descriptors to be written before UDC bind.
	for _, ffsFunc := range g.FFSFunctions() {
		log.Printf("[FFS] 初始化 FFS %s ...", ffsFunc.Name)
		if err := setupFFS(ffsFunc, configs, devDesc); err != nil {
			return fmt.Errorf("初始化 FFS: %w", err)
		}
	}

	log.Println("[Gadget] 连接 UDC ...")
	if err := g.ConnectUDC(); err != nil {
		return fmt.Errorf("连接 UDC: %w", err)
	}
	log.Println("[Gadget] 已连接到主机")

	p, pipeCtx := pipe.NewWithContext(ctx, 64)
	defer p.Close()

	// Start FFS I/O (open endpoints + data flow) AFTER UDC connect.
	for _, ffsFunc := range g.FFSFunctions() {
		log.Printf("[FFS] 启动 FFS I/O %s ...", ffsFunc.Name)
		startFFSIO(pipeCtx, ffsFunc, configs, p, debug)
	}

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
