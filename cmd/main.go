package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "linux-cu",
		Short: "USB HID 透传模拟工具 (Linux Gadget usb_f_hid)",
	}
	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(emulateCmd())
	rootCmd.AddCommand(saveCmd())
	rootCmd.AddCommand(loadCmd())
	rootCmd.AddCommand(sendCmd())
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
	var udsAddr string
	var udpAddr string

	cmd := &cobra.Command{
		Use:   "emulate <device>",
		Short: "模拟指定 USB 设备 (通过 Gadget HID)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			busNum, devAddr, err := resolveDevice(args[0])
			if err != nil {
				return err
			}
			return runEmulate(busNum, devAddr, debug, udsAddr, udpAddr)
		},
	}
	cmd.Flags().Bool("debug", false, "显示真实设备与虚拟设备之间的所有交互数据")
	cmd.Flags().StringVar(&udsAddr, "uds", "", "Unix Domain Socket 地址，接收外部事件注入 (如 /tmp/hid.sock; @前缀表示抽象套接字如 @hid)")
	cmd.Flags().StringVar(&udpAddr, "udp", "", "UDP 地址，接收外部事件注入 (如 :9090 监听所有IP; 127.0.0.1:9090 监听指定IP)")
	return cmd
}

// resolveDevice parses a device specifier and returns (busNum, devAddr).
// Accepts formats:
//   - "2:3"    -> bus:dev (decimal)
//   - "054c:0ce6" -> VID:PID (hex, selects first match)
func resolveDevice(spec string) (int, int, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("无效设备标识 %q，格式为 bus:dev 或 vid:pid", spec)
	}

	// Try bus:dev first (both parts are decimal integers)
	bus, busErr := strconv.Atoi(parts[0])
	dev, devErr := strconv.Atoi(parts[1])
	if busErr == nil && devErr == nil && bus > 0 && dev > 0 {
		return bus, dev, nil
	}

	// Try VID:PID (hex)
	vid, vidErr := strconv.ParseUint(parts[0], 16, 16)
	pid, pidErr := strconv.ParseUint(parts[1], 16, 16)
	if vidErr == nil && pidErr == nil {
		b, d, err := usbFindDevice(uint16(vid), uint16(pid))
		if err != nil {
			return 0, 0, fmt.Errorf("查找设备 VID:PID=%s: %w", spec, err)
		}
		return b, d, nil
	}

	return 0, 0, fmt.Errorf("无法解析设备标识 %q (非 bus:dev 也非 vid:pid)", spec)
}

func saveCmd() *cobra.Command {
	var outFile string

	cmd := &cobra.Command{
		Use:   "save <device>",
		Short: "保存指定 USB 设备信息为 YAML 文件",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			busNum, devAddr, err := resolveDevice(args[0])
			if err != nil {
				return err
			}
			return runSave(busNum, devAddr, outFile)
		},
	}
	cmd.Flags().StringVarP(&outFile, "output", "o", "", "输出 YAML 文件路径 (默认: <vid_pid>.yaml)")
	return cmd
}

func loadCmd() *cobra.Command {
	var udsAddr string
	var udpAddr string
	var recvMode bool

	cmd := &cobra.Command{
		Use:   "load <yaml-file>",
		Short: "从 YAML 文件创建 Gadget 设备并运行",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			return runLoad(args[0], debug, udsAddr, udpAddr, recvMode)
		},
	}
	cmd.Flags().Bool("debug", false, "显示真实设备与虚拟设备之间的所有交互数据")
	cmd.Flags().StringVar(&udsAddr, "uds", "", "Unix Domain Socket 地址，接收外部事件注入 (如 /tmp/hid.sock; @前缀表示抽象套接字如 @hid)")
	cmd.Flags().StringVar(&udpAddr, "udp", "", "UDP 地址，接收外部事件注入 (如 :9090 监听所有IP; 127.0.0.1:9090 监听指定IP)")
	cmd.Flags().BoolVar(&recvMode, "recv", false, "双向 IPC 模式：接收注入数据，同时将 HIDG 主机输出回传给最近来源")
	return cmd
}

func sendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send <device> <target>",
		Short: "发送模式：读取 USB 设备数据发送到网络目标，并接收网络数据写回设备",
		Long: `发送模式：不创建 Gadget 设备，仅读取指定 USB 设备的数据，
通过 UDS 或 UDP 发送到指定目标，并接收远端数据写回设备。

设备格式与 emulate 相同：bus:dev 或 VID:PID
目标格式：UDS:address 或 UDP:address
  UDS 示例: UDS:@hid  UDS:/tmp/hid.socket
  UDP 示例: UDP:192.168.3.3:9981  UDP:127.0.0.1:9999`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			return runSend(args[0], args[1], debug)
		},
	}
	cmd.Flags().Bool("debug", false, "显示所有交互数据")
	return cmd
}
