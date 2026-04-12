package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/linux-cu/client/pkg/session"
	"github.com/linux-cu/client/pkg/usb"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "usb-reader",
		Short: "USB HID 直通仿真工具",
	}

	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(listCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func startCmd() *cobra.Command {
	var serialDev string
	var debug bool
	var busNum int
	var devAddr int

	cmd := &cobra.Command{
		Use:   "start",
		Short: "启动 USB HID 直通仿真",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			if serialDev == "" {
				detected, err := waitForSerialDevice(ctx)
				if err != nil {
					return err
				}
				serialDev = detected
			}

			if busNum == 0 || devAddr == 0 {
				return fmt.Errorf("必须指定 --bus 和 --dev 参数 (使用 list 命令查看设备)")
			}

			cfg := session.Config{
				SerialDevice: serialDev,
				Debug:        debug,
				BusNumber:    busNum,
				DevAddress:   devAddr,
			}

			log.Printf("连接 %s (4M baud)", serialDev)
			s := session.New(cfg)
			return s.Run(ctx)
		},
	}

	cmd.Flags().StringVarP(&serialDev, "serial", "s", "", "串口设备路径 (例如 /dev/ttyACM0)")
	cmd.Flags().BoolVar(&debug, "debug", false, "调试模式 (115200 波特率)")
	cmd.Flags().IntVar(&busNum, "bus", 0, "USB 总线号")
	cmd.Flags().IntVar(&devAddr, "dev", 0, "USB 设备地址")

	return cmd
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出 USB 设备",
		RunE: func(cmd *cobra.Command, args []string) error {
			devices, err := usb.ListDevices()
			if err != nil {
				return err
			}
			usb.PrintDeviceInfo(devices)
			return nil
		},
	}
}

func waitForSerialDevice(ctx context.Context) (string, error) {
	log.Println("[串口] 自动检测 /dev/ttyACM* ...")
	if dev := findSerialDevice(); dev != "" {
		log.Printf("[串口] 检测到: %s", dev)
		return dev, nil
	}
	log.Println("[串口] 等待设备插入... (Ctrl+C 取消)")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			if dev := findSerialDevice(); dev != "" {
				log.Printf("[串口] 检测到: %s", dev)
				return dev, nil
			}
		}
	}
}

func findSerialDevice() string {
	matches, err := filepath.Glob("/dev/ttyACM*")
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}
