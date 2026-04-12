package usb

import (
	"fmt"
	"strings"
)

// PrintDeviceInfo 打印设备简要信息（用于 list 命令）
func PrintDeviceInfo(devices []DeviceInfo) {
	fmt.Printf("%-6s %-9s %-8s %-24s %-24s %s\n",
		"Bus:Dev", "VID:PID", "Class", "Manufacturer", "Product", "Serial")
	fmt.Println(strings.Repeat("-", 100))

	for _, d := range devices {
		mfg := d.Manufacturer
		if mfg == "" {
			mfg = "-"
		}
		prod := d.Product
		if prod == "" {
			prod = "-"
		}
		serial := d.SerialNumber
		if serial == "" {
			serial = "-"
		}
		fmt.Printf("%d:%-5d %04x:%04x 0x%02x     %-24s %-24s %s\n",
			d.BusNumber, d.DevAddress,
			d.VendorID, d.ProductID, d.DeviceClass,
			truncate(mfg, 24), truncate(prod, 24), serial)
	}
}

// PrintFullDescriptors 打印完整描述符信息
func PrintFullDescriptors(dd DeviceDescriptor, configs []ConfigDescriptor) {
	printDeviceDescriptor(dd)
	for i, cfg := range configs {
		printConfigDescriptor(i, cfg)
	}
}

func printDeviceDescriptor(dd DeviceDescriptor) {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    Device Descriptor                        ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  bcdUSB:          %d.%d%02d\n", dd.BcdUSB>>8, (dd.BcdUSB>>4)&0xF, dd.BcdUSB&0xF)
	fmt.Printf("║  bDeviceClass:    0x%02X\n", dd.DeviceClass)
	fmt.Printf("║  bDeviceSubClass: 0x%02X\n", dd.DeviceSubClass)
	fmt.Printf("║  bDeviceProtocol: 0x%02X\n", dd.DeviceProtocol)
	fmt.Printf("║  bMaxPacketSize0: %d\n", dd.MaxPacketSize0)
	fmt.Printf("║  idVendor:        0x%04X\n", dd.VendorID)
	fmt.Printf("║  idProduct:       0x%04X\n", dd.ProductID)
	fmt.Printf("║  bcdDevice:       %d.%d%02d\n", dd.BcdDevice>>8, (dd.BcdDevice>>4)&0xF, dd.BcdDevice&0xF)
	fmt.Printf("║  iManufacturer:   %s\n", dd.Manufacturer)
	fmt.Printf("║  iProduct:        %s\n", dd.Product)
	fmt.Printf("║  iSerialNumber:   %s\n", dd.SerialNumber)
	fmt.Printf("║  bNumConfigs:     %d\n", dd.NumConfigs)
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Raw (18 bytes):")
	printHexDump(dd.Raw, "║    ")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()
}

func printConfigDescriptor(idx int, cd ConfigDescriptor) {
	fmt.Printf("╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║             Configuration Descriptor #%d                     ║\n", idx)
	fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  bConfigurationValue: %d\n", cd.ConfigValue)
	fmt.Printf("║  iConfiguration:      %s\n", cd.ConfigString)
	fmt.Printf("║  bmAttributes:        0x%02X", cd.MaxPower)
	parts := []string{}
	if cd.SelfPowered {
		parts = append(parts, "Self-Powered")
	}
	if cd.RemoteWakeup {
		parts = append(parts, "Remote-Wakeup")
	}
	if len(parts) > 0 {
		fmt.Printf(" (%s)", strings.Join(parts, ", "))
	}
	fmt.Println()
	fmt.Printf("║  bNumInterfaces:      %d\n", cd.NumInterfaces)
	fmt.Printf("║  MaxPower:            %dmA\n", cd.MaxPower*2)
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Raw:")
	printHexDump(cd.Raw, "║    ")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	for i, iface := range cd.Interfaces {
		printInterfaceDescriptor(i, iface)
	}
}

func printInterfaceDescriptor(idx int, iface InterfaceDescriptor) {
	fmt.Printf("  ┌────────────────────────────────────────────────────────────┐\n")
	fmt.Printf("  │             Interface #%d (Alt Setting %d)                  │\n",
		iface.InterfaceNumber, iface.AlternateSetting)
	fmt.Printf("  ├────────────────────────────────────────────────────────────┤\n")
	fmt.Printf("  │  bInterfaceClass:    0x%02X (%s)\n", iface.InterfaceClass, usbClassName(iface.InterfaceClass))
	fmt.Printf("  │  bInterfaceSubClass: 0x%02X\n", iface.InterfaceSubClass)
	fmt.Printf("  │  bInterfaceProtocol: 0x%02X\n", iface.InterfaceProtocol)
	fmt.Printf("  │  iInterface:         %s\n", iface.InterfaceString)
	fmt.Printf("  │  bNumEndpoints:      %d\n", iface.NumEndpoints)
	fmt.Printf("  ├────────────────────────────────────────────────────────────┤\n")

	for _, ep := range iface.Endpoints {
		printEndpointDescriptor(ep)
	}

	if len(iface.ReportDescriptor) > 0 {
		fmt.Printf("  ├────────────────────────────────────────────────────────────┤\n")
		fmt.Printf("  │  HID Report Descriptor (%d bytes):\n", len(iface.ReportDescriptor))
		printHexDump(iface.ReportDescriptor, "  │    ")
	}

	fmt.Printf("  └────────────────────────────────────────────────────────────┘\n")
	fmt.Println()
}

func printEndpointDescriptor(ep EndpointDescriptor) {
	dir := "IN"
	if ep.EndpointAddress&0x80 == 0 {
		dir = "OUT"
	}
	epType := []string{"Control", "Isochronous", "Bulk", "Interrupt"}[ep.Attributes&0x03]
	fmt.Printf("  │  Endpoint 0x%02X (%s %s) MaxPacket=%d Interval=%d\n",
		ep.EndpointAddress, dir, epType, ep.MaxPacketSize, ep.Interval)
}

// printHexDump 十六进制 dump
func printHexDump(data []byte, prefix string) {
	for i := 0; i < len(data); i += 16 {
		fmt.Print(prefix)
		// 十六进制
		for j := 0; j < 16; j++ {
			if i+j < len(data) {
				fmt.Printf("%02X ", data[i+j])
			} else {
				fmt.Print("   ")
			}
			if j == 7 {
				fmt.Print(" ")
			}
		}
		fmt.Print(" |")
		// ASCII
		for j := 0; j < 16 && i+j < len(data); j++ {
			b := data[i+j]
			if b >= 0x20 && b <= 0x7E {
				fmt.Printf("%c", b)
			} else {
				fmt.Print(".")
			}
		}
		fmt.Println("|")
	}
}

func usbClassName(class uint8) string {
	names := map[uint8]string{
		0x00: "Defined at Interface",
		0x01: "Audio",
		0x02: "CDC",
		0x03: "HID",
		0x05: "Physical",
		0x06: "Image",
		0x07: "Printer",
		0x08: "Mass Storage",
		0x09: "Hub",
		0x0A: "CDC-Data",
		0x0B: "Smart Card",
		0x0D: "Content Security",
		0x0E: "Video",
		0x0F: "Personal Healthcare",
		0x10: "Audio/Video",
		0xDC: "Diagnostic",
		0xE0: "Wireless",
		0xEF: "Miscellaneous",
		0xFE: "Application Specific",
		0xFF: "Vendor Specific",
	}
	if name, ok := names[class]; ok {
		return name
	}
	return "Unknown"
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
