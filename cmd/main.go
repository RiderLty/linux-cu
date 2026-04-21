package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "linux-cu",
		Short: "USB HID 透传模拟工具 (Linux Gadget FunctionFS)",
	}
	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(emulateCmd())
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出所有 USB 设备",
		RunE: func(cmd *cobra.Command, args []string) error {
			devices, err := usbListDevices()
			if err != nil {
				return err
			}
			usbPrintDeviceInfo(devices)
			return nil
		},
	}
}

func emulateCmd() *cobra.Command {
	var busNum int
	var devAddr int
	var vidHex string
	var pidHex string

	cmd := &cobra.Command{
		Use:   "emulate",
		Short: "模拟指定 USB 设备 (通过 Gadget HID)",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			return runEmulateRoot(busNum, devAddr, vidHex, pidHex, debug)
		},
	}
	cmd.Flags().IntVar(&busNum, "bus", 0, "USB 总线号")
	cmd.Flags().IntVar(&devAddr, "dev", 0, "USB 设备地址")
	cmd.Flags().StringVar(&vidHex, "vid", "", "Vendor ID (hex, e.g. 046d)")
	cmd.Flags().StringVar(&pidHex, "pid", "", "Product ID (hex, e.g. c08b)")
	cmd.Flags().Bool("debug", false, "显示真实设备与虚拟设备之间的所有交互数据")
	return cmd
}

func runEmulateRoot(busNum int, devAddr int, vidHex, pidHex string, debug bool) error {
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
	return runEmulate(busNum, devAddr, debug)
}
