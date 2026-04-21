package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/linux-cu/pkg/gadget"
	"github.com/linux-cu/pkg/pipe"
	"github.com/linux-cu/pkg/profile"
)

func runLoad(yamlPath string, debug bool, udsAddr, udpAddr string) error {
	p, err := profile.Load(yamlPath)
	if err != nil {
		return err
	}

	gadgetCfg := p.ToGadgetConfig()
	maxPower, selfPowered, remoteWakeup := p.ConfigAttrs()
	gadgetCfg.MaxPower = maxPower
	gadgetCfg.SelfPowered = selfPowered
	gadgetCfg.RemoteWakeup = remoteWakeup

	log.Printf("[Gadget] 创建 VID=%s PID=%s", p.Device.VendorID, p.Device.ProductID)
	g, err := gadget.Create(gadgetCfg)
	if err != nil {
		return err
	}
	defer g.Destroy()
	log.Printf("[Gadget] 已创建: %s", g.Name)

	// Add HID functions
	hidIfaces := p.HIDInterfaces()
	for i, hi := range hidIfaces {
		hidCfg := gadget.HIDConfig{
			Instance:   fmt.Sprintf("usb%d", i),
			Protocol:   hi.Protocol,
			SubClass:   hi.SubClass,
			ReportLen:  hi.ReportLen,
			ReportDesc: hi.ReportDesc,
		}
		log.Printf("[Gadget] 添加 HID 功能 usb%d (iface=%d, protocol=%d, subclass=%d, report_len=%d, report_desc=%d bytes)",
			i, hi.InterfaceNumber, hi.Protocol, hi.SubClass, hi.ReportLen, len(hi.ReportDesc))
		if err := g.AddHIDFunction(hi.InterfaceNumber, hidCfg); err != nil {
			return err
		}
	}

	log.Println("[Gadget] 连接 UDC ...")
	if err := g.ConnectUDC(); err != nil {
		return err
	}
	log.Println("[Gadget] 已连接到主机")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[主] 收到退出信号，清理中...")
		cancel()
	}()

	pipeObj, pipeCtx := pipe.NewWithContext(ctx, 64)
	defer pipeObj.Close()

	startGadgetIO(pipeCtx, g, pipeObj, 0, 0, debug)

	// Start IPC injection if configured
	if udsAddr != "" {
		startIPCInjection(pipeCtx, "uds", udsAddr, pipeObj, g.IfaceToHidIdx, debug)
	}
	if udpAddr != "" {
		startIPCInjection(pipeCtx, "udp", udpAddr, pipeObj, g.IfaceToHidIdx, debug)
	}

	log.Println("[主] 进入主循环，Ctrl+C 退出 (无真实设备透传，仅支持 IPC 注入)")
	<-pipeCtx.Done()
	log.Println("[主] 退出")
	return nil
}
