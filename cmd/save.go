package main

import (
	"fmt"
	"log"

	"github.com/linux-cu/pkg/profile"
	"github.com/linux-cu/pkg/usb"
)

func runSave(busNum, devAddr int, vidHex, pidHex, outFile string) error {
	if vidHex != "" && pidHex != "" {
		vid, pid, err := parseVIDPID(vidHex, pidHex)
		if err != nil {
			return err
		}
		bus, dev, err := usbFindDevice(vid, pid)
		if err != nil {
			return fmt.Errorf("查找设备 VID:PID=%s:%s: %w", vidHex, pidHex, err)
		}
		busNum = bus
		devAddr = dev
	}
	if busNum == 0 || devAddr == 0 {
		return fmt.Errorf("必须指定 --bus 和 --dev 或 --vid 和 --pid")
	}

	log.Printf("[描述符] 读取设备 %d:%d ...", busNum, devAddr)
	configs, devDesc, err := usb.ReadDescriptors(busNum, devAddr)
	if err != nil {
		return fmt.Errorf("读取描述符: %w", err)
	}

	p := profile.FromDescriptors(devDesc, configs)

	if outFile == "" {
		outFile = fmt.Sprintf("%04x_%04x.yaml", devDesc.VendorID, devDesc.ProductID)
	}

	if err := p.Save(outFile); err != nil {
		return fmt.Errorf("保存文件: %w", err)
	}

	log.Printf("[保存] 已保存到 %s", outFile)
	return nil
}
