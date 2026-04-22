package profile

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/linux-cu/pkg/usb"
)

func TestFromDescriptors(t *testing.T) {
	devDesc := usb.DeviceDescriptor{
		VendorID:      0x045e,
		ProductID:     0x02ea,
		BcdUSB:        0x0200,
		BcdDevice:     0x050d,
		DeviceClass:   0xff,
		DeviceSubClass: 0x47,
		DeviceProtocol: 0xd0,
		MaxPacketSize0: 64,
		Manufacturer:  "Microsoft",
		Product:       "Controller",
		SerialNumber:  "AABBCCDD",
	}

	configs := []usb.ConfigDescriptor{
		{
			ConfigValue:  1,
			MaxPower:      50,
			SelfPowered:   false,
			RemoteWakeup:  true,
			NumInterfaces: 3,
			Interfaces: []usb.InterfaceDescriptor{
				{
					InterfaceNumber:   0,
					AlternateSetting:  0,
					InterfaceClass:    0xff,
					InterfaceSubClass: 0x47,
					InterfaceProtocol: 0xd0,
					NumEndpoints:      2,
					Endpoints: []usb.EndpointDescriptor{
						{EndpointAddress: 0x02, Attributes: 0x03, MaxPacketSize: 64, Interval: 4},
						{EndpointAddress: 0x82, Attributes: 0x03, MaxPacketSize: 64, Interval: 4},
					},
				},
				{
					InterfaceNumber:   1,
					AlternateSetting:  0,
					InterfaceClass:    0xff,
					InterfaceSubClass: 0x47,
					InterfaceProtocol: 0xd0,
					NumEndpoints:      0,
				},
				{
					InterfaceNumber:   1,
					AlternateSetting:  1,
					InterfaceClass:    0xff,
					InterfaceSubClass: 0x47,
					InterfaceProtocol: 0xd0,
					NumEndpoints:      2,
					Endpoints: []usb.EndpointDescriptor{
						{EndpointAddress: 0x03, Attributes: 0x01, MaxPacketSize: 228, Interval: 1},
						{EndpointAddress: 0x83, Attributes: 0x01, MaxPacketSize: 64, Interval: 1},
					},
				},
				{
					InterfaceNumber:   2,
					AlternateSetting:  0,
					InterfaceClass:    0xff,
					InterfaceSubClass: 0x47,
					InterfaceProtocol: 0xd0,
					NumEndpoints:      0,
				},
				{
					InterfaceNumber:   2,
					AlternateSetting:  1,
					InterfaceClass:    0xff,
					InterfaceSubClass: 0x47,
					InterfaceProtocol: 0xd0,
					NumEndpoints:      2,
					Endpoints: []usb.EndpointDescriptor{
						{EndpointAddress: 0x04, Attributes: 0x02, MaxPacketSize: 64, Interval: 0},
						{EndpointAddress: 0x84, Attributes: 0x02, MaxPacketSize: 64, Interval: 0},
					},
				},
			},
		},
	}

	p := FromDescriptors(devDesc, configs)

	// Check device descriptor
	if p.Device.VendorID != "0x045e" {
		t.Errorf("VendorID = %s, want 0x045e", p.Device.VendorID)
	}
	if p.Device.ProductID != "0x02ea" {
		t.Errorf("ProductID = %s, want 0x02ea", p.Device.ProductID)
	}
	if p.Device.Manufacturer != "Microsoft" {
		t.Errorf("Manufacturer = %s, want Microsoft", p.Device.Manufacturer)
	}

	// Check configs
	if len(p.Configs) != 1 {
		t.Fatalf("len(Configs) = %d, want 1", len(p.Configs))
	}
	cfg := p.Configs[0]
	if cfg.ConfigValue != 1 {
		t.Errorf("ConfigValue = %d, want 1", cfg.ConfigValue)
	}
	if cfg.MaxPower != 50 {
		t.Errorf("MaxPower = %d, want 50", cfg.MaxPower)
	}

	// Check interfaces - should have 5 (all alt settings)
	if len(cfg.Interfaces) != 5 {
		t.Errorf("len(Interfaces) = %d, want 5", len(cfg.Interfaces))
	}

	// Check interface 0 endpoints
	iface0 := cfg.Interfaces[0]
	if len(iface0.Endpoints) != 2 {
		t.Errorf("iface0 endpoints = %d, want 2", len(iface0.Endpoints))
	}
	if iface0.Endpoints[0].EndpointAddress != 0x02 {
		t.Errorf("iface0 ep0 address = 0x%02x, want 0x02", iface0.Endpoints[0].EndpointAddress)
	}

	// No HID interfaces in Xbox controller
	hidIfaces := p.HIDInterfaces()
	if len(hidIfaces) != 0 {
		t.Errorf("HIDInterfaces = %d, want 0 (Xbox has no HID)", len(hidIfaces))
	}

	// ToGadgetConfig
	gcfg := p.ToGadgetConfig()
	if gcfg.VID != 0x045e {
		t.Errorf("VID = 0x%04x, want 0x045e", gcfg.VID)
	}
	if gcfg.PID != 0x02ea {
		t.Errorf("PID = 0x%04x, want 0x02ea", gcfg.PID)
	}

	// ConfigAttrs
	mp, sp, rw := p.ConfigAttrs()
	if mp != 50 {
		t.Errorf("MaxPower = %d, want 50", mp)
	}
	if sp != false {
		t.Errorf("SelfPowered = %v, want false", sp)
	}
	if rw != true {
		t.Errorf("RemoteWakeup = %v, want true", rw)
	}
}

func TestHIDDevice(t *testing.T) {
	reportDesc, _ := hex.DecodeString("05010905a1018501093009310932093509330934150026ff00750895068102c0")

	devDesc := usb.DeviceDescriptor{
		VendorID:    0x046d,
		ProductID:   0xc547,
		BcdUSB:      0x0200,
		Manufacturer: "Logitech",
		Product:     "USB Receiver",
	}

	configs := []usb.ConfigDescriptor{
		{
			ConfigValue:  1,
			MaxPower:      49,
			Interfaces: []usb.InterfaceDescriptor{
				{
					InterfaceNumber:   0,
					AlternateSetting:  0,
					InterfaceClass:    0x03,
					InterfaceSubClass: 0x01,
					InterfaceProtocol: 0x02,
					Endpoints: []usb.EndpointDescriptor{
						{EndpointAddress: 0x81, Attributes: 0x03, MaxPacketSize: 64, Interval: 4},
					},
					ReportDescriptor: reportDesc,
				},
				{
					InterfaceNumber:   1,
					AlternateSetting:  0,
					InterfaceClass:    0x03,
					InterfaceSubClass: 0x01,
					InterfaceProtocol: 0x01,
					Endpoints: []usb.EndpointDescriptor{
						{EndpointAddress: 0x82, Attributes: 0x03, MaxPacketSize: 64, Interval: 4},
					},
					ReportDescriptor: reportDesc,
				},
			},
		},
	}

	p := FromDescriptors(devDesc, configs)

	// Should have 2 HID interfaces
	hidIfaces := p.HIDInterfaces()
	if len(hidIfaces) != 2 {
		t.Fatalf("HIDInterfaces = %d, want 2", len(hidIfaces))
	}

	// Check first HID interface
	if hidIfaces[0].InterfaceNumber != 0 {
		t.Errorf("HID iface0 number = %d, want 0", hidIfaces[0].InterfaceNumber)
	}
	if hidIfaces[0].Protocol != 0x02 {
		t.Errorf("HID iface0 protocol = %d, want 2", hidIfaces[0].Protocol)
	}
	if hidIfaces[0].ReportLen != 64 {
		t.Errorf("HID iface0 report_len = %d, want 64", hidIfaces[0].ReportLen)
	}
	if len(hidIfaces[0].ReportDesc) != len(reportDesc) {
		t.Errorf("HID iface0 report_desc len = %d, want %d", len(hidIfaces[0].ReportDesc), len(reportDesc))
	}
}

func TestSaveLoad(t *testing.T) {
	devDesc := usb.DeviceDescriptor{
		VendorID:    0x045e,
		ProductID:   0x02ea,
		BcdUSB:      0x0200,
		BcdDevice:   0x050d,
		Manufacturer: "Microsoft",
		Product:     "Controller",
	}

	configs := []usb.ConfigDescriptor{
		{
			ConfigValue: 1,
			MaxPower:    50,
			Interfaces: []usb.InterfaceDescriptor{
				{
					InterfaceNumber:   0,
					AlternateSetting:  0,
					InterfaceClass:    0xff,
					InterfaceSubClass: 0x47,
					InterfaceProtocol: 0xd0,
					Endpoints: []usb.EndpointDescriptor{
						{EndpointAddress: 0x02, Attributes: 0x03, MaxPacketSize: 64, Interval: 4},
						{EndpointAddress: 0x82, Attributes: 0x03, MaxPacketSize: 64, Interval: 4},
					},
				},
			},
		},
	}

	p := FromDescriptors(devDesc, configs)

	// Save
	tmpFile := t.TempDir() + "/test.yaml"
	if err := p.Save(tmpFile); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load
	p2, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify
	if p2.Device.VendorID != p.Device.VendorID {
		t.Errorf("VendorID mismatch: %s vs %s", p2.Device.VendorID, p.Device.VendorID)
	}
	if p2.Device.ProductID != p.Device.ProductID {
		t.Errorf("ProductID mismatch: %s vs %s", p2.Device.ProductID, p.Device.ProductID)
	}
	if len(p2.Configs) != 1 {
		t.Fatalf("Configs count = %d, want 1", len(p2.Configs))
	}
	if len(p2.Configs[0].Interfaces) != 1 {
		t.Errorf("Interfaces count = %d, want 1", len(p2.Configs[0].Interfaces))
	}

	// Verify gadget config conversion
	gcfg := p2.ToGadgetConfig()
	if gcfg.VID != 0x045e {
		t.Errorf("VID = 0x%04x, want 0x045e", gcfg.VID)
	}
	if gcfg.PID != 0x02ea {
		t.Errorf("PID = 0x%04x, want 0x02ea", gcfg.PID)
	}

	// Verify file exists and is valid YAML
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Error("YAML file is empty")
	}
}

func TestParseHex16(t *testing.T) {
	tests := []struct {
		input string
		want  uint16
	}{
		{"0x045e", 0x045e},
		{"0x02ea", 0x02ea},
		{"0x0000", 0x0000},
		{"0xffff", 0xffff},
	}

	for _, tt := range tests {
		got := parseHex16(tt.input)
		if got != tt.want {
			t.Errorf("parseHex16(%q) = 0x%04x, want 0x%04x", tt.input, got, tt.want)
		}
	}
}
