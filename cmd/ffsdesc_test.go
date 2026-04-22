package main

import (
	"testing"

	"github.com/linux-cu/pkg/usb"
)

func TestBuildNonHIDFFSDescriptors(t *testing.T) {
	// Xbox controller: all non-HID interfaces
	configs := []usb.ConfigDescriptor{
		{
			ConfigValue: 1,
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
					InterfaceNumber:   2,
					AlternateSetting:  0,
					InterfaceClass:    0xff,
					InterfaceSubClass: 0x47,
					InterfaceProtocol: 0xd0,
					NumEndpoints:      0,
				},
			},
		},
	}

	fsDescs, hsDescs, counts, ifaceNums := buildNonHIDFFSDescriptors(configs)

	if len(fsDescs) == 0 {
		t.Fatal("fsDescs is empty")
	}
	if len(hsDescs) == 0 {
		t.Fatal("hsDescs is empty")
	}
	if counts.FSCount == 0 {
		t.Error("FSCount = 0, expected non-zero")
	}
	if counts.HSCount == 0 {
		t.Error("HSCount = 0, expected non-zero")
	}

	// Should have 3 unique non-HID interface numbers (from alt0 only)
	expectedIfaces := []uint8{0, 1, 2}
	if len(ifaceNums) != len(expectedIfaces) {
		t.Errorf("ifaceNums = %v, want %v", ifaceNums, expectedIfaces)
	}
	for i, n := range ifaceNums {
		if n != expectedIfaces[i] {
			t.Errorf("ifaceNums[%d] = %d, want %d", i, n, expectedIfaces[i])
		}
	}

	// Verify descriptor blob: 3 interfaces (9 bytes each) + 2 endpoints on iface0 (7 bytes each)
	// = 3*9 + 2*7 = 41 bytes
	expectedBlobLen := 3*9 + 2*7
	if len(fsDescs[0]) != expectedBlobLen {
		t.Errorf("fs blob len = %d, want %d", len(fsDescs[0]), expectedBlobLen)
	}

	// Verify first interface descriptor
	blob := fsDescs[0]
	if blob[0] != 9 { // bLength
		t.Errorf("iface bLength = %d, want 9", blob[0])
	}
	if blob[1] != 4 { // bDescriptorType
		t.Errorf("iface bDescriptorType = %d, want 4", blob[1])
	}
	if blob[5] != 0xff { // bInterfaceClass
		t.Errorf("iface bInterfaceClass = 0x%02x, want 0xff", blob[5])
	}
}

func TestBuildNonHIDFFSDescriptorsMixed(t *testing.T) {
	// Mixed device: HID + non-HID
	configs := []usb.ConfigDescriptor{
		{
			ConfigValue: 1,
			Interfaces: []usb.InterfaceDescriptor{
				{
					InterfaceNumber:   0,
					AlternateSetting:  0,
					InterfaceClass:    0x03, // HID
					InterfaceSubClass: 0x01,
					InterfaceProtocol: 0x02,
					Endpoints: []usb.EndpointDescriptor{
						{EndpointAddress: 0x81, Attributes: 0x03, MaxPacketSize: 64, Interval: 4},
					},
					ReportDescriptor: []byte{0x05, 0x01},
				},
				{
					InterfaceNumber:   1,
					AlternateSetting:  0,
					InterfaceClass:    0xff, // non-HID
					InterfaceSubClass: 0x47,
					InterfaceProtocol: 0xd0,
					Endpoints: []usb.EndpointDescriptor{
						{EndpointAddress: 0x02, Attributes: 0x02, MaxPacketSize: 64, Interval: 0},
						{EndpointAddress: 0x82, Attributes: 0x02, MaxPacketSize: 64, Interval: 0},
					},
				},
			},
		},
	}

	fsDescs, _, counts, ifaceNums := buildNonHIDFFSDescriptors(configs)

	// Should only have 1 non-HID interface
	if len(ifaceNums) != 1 {
		t.Errorf("ifaceNums = %v, want [1]", ifaceNums)
	}
	if ifaceNums[0] != 1 {
		t.Errorf("ifaceNums[0] = %d, want 1", ifaceNums[0])
	}

	// 1 interface (9 bytes) + 2 endpoints (7 bytes each) = 23 bytes
	expectedBlobLen := 9 + 2*7
	if len(fsDescs[0]) != expectedBlobLen {
		t.Errorf("fs blob len = %d, want %d", len(fsDescs[0]), expectedBlobLen)
	}

	// Count should be 3 (1 iface + 2 endpoints)
	if counts.FSCount != 3 {
		t.Errorf("FSCount = %d, want 3", counts.FSCount)
	}
}

func TestBuildNonHIDFFSDescriptorsAllHID(t *testing.T) {
	// Pure HID device - no FFS needed
	configs := []usb.ConfigDescriptor{
		{
			ConfigValue: 1,
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
				},
			},
		},
	}

	fsDescs, _, _, ifaceNums := buildNonHIDFFSDescriptors(configs)

	if len(fsDescs) != 0 {
		t.Errorf("fsDescs should be empty for pure HID device, got %d blobs", len(fsDescs))
	}
	if len(ifaceNums) != 0 {
		t.Errorf("ifaceNums should be empty for pure HID device, got %v", ifaceNums)
	}
}

func TestCountDescriptors(t *testing.T) {
	// 1 interface (9 bytes) + 2 endpoints (7 bytes each)
	blob := make([]byte, 0, 23)
	iface := []byte{9, 4, 0, 0, 2, 0xff, 0x47, 0xd0, 0}
	ep1 := []byte{7, 5, 0x02, 0x03, 64, 0, 4}
	ep2 := []byte{7, 5, 0x82, 0x03, 64, 0, 4}
	blob = append(blob, iface...)
	blob = append(blob, ep1...)
	blob = append(blob, ep2...)

	count := countDescriptors(blob)
	if count != 3 {
		t.Errorf("countDescriptors = %d, want 3", count)
	}
}
