package main

import (
	"fmt"
	"log"

	"github.com/linux-cu/pkg/profile"
	"github.com/linux-cu/pkg/usb"
)

func runSave(busNum, devAddr int, outFile string) error {
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
