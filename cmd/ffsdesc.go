package main

import (
	"github.com/linux-cu/pkg/gadget"
	"github.com/linux-cu/pkg/usb"
)

// buildFFSDescriptors builds raw descriptor blobs for FunctionFS.
// FunctionFS expects a flat concatenation of all descriptors per speed.
// The count field is the total number of individual descriptors
// (each interface, endpoint, and class descriptor counts as one).
func buildFFSDescriptors(configs []usb.ConfigDescriptor) (fsDescs, hsDescs [][]byte) {
	if len(configs) == 0 {
		return nil, nil
	}
	cfg := configs[0]

	// Build flat descriptor blob and count total descriptors
	var fsBlob, hsBlob []byte
	for _, iface := range cfg.Interfaces {
		fsBlob = append(fsBlob, buildIfaceDesc(iface)...)
		hsBlob = append(hsBlob, buildIfaceDesc(iface)...)
		for _, ep := range iface.Endpoints {
			fsBlob = append(fsBlob, buildEPDesc(ep)...)
			hsBlob = append(hsBlob, buildEPDesc(ep)...)
		}
		if iface.InterfaceClass == 0x03 && len(iface.ReportDescriptor) > 0 {
			fsBlob = append(fsBlob, buildHIDDesc(iface.ReportDescriptor)...)
			hsBlob = append(hsBlob, buildHIDDesc(iface.ReportDescriptor)...)
		}
	}

	// Count total descriptors by walking the blob
	fsCount := countDescriptors(fsBlob)
	hsCount := countDescriptors(hsBlob)

	// Return as single-element slices (each speed has one blob)
	fsDescs = [][]byte{fsBlob}
	hsDescs = [][]byte{hsBlob}
	// Store counts for header building
	// We need to pass the counts somehow... let's use a side channel
	// Actually, the buildDescriptorsBlob already counts len(fsDescs) as the count,
	// so we need to change the approach. Let's pass the counts directly.
	_ = fsCount
	_ = hsCount
	return
}

// ffsDescCounts stores descriptor counts per speed
type ffsDescCounts struct {
	FSCount int
	HSCount int
}

// buildFFSDescriptorsWithCounts returns both the descriptor blobs and the individual descriptor counts
func buildFFSDescriptorsWithCounts(configs []usb.ConfigDescriptor) (fsDescs, hsDescs [][]byte, counts ffsDescCounts) {
	if len(configs) == 0 {
		return nil, nil, counts
	}
	cfg := configs[0]

	var fsBlob, hsBlob []byte
	for _, iface := range cfg.Interfaces {
		fsBlob = append(fsBlob, buildIfaceDesc(iface)...)
		hsBlob = append(hsBlob, buildIfaceDesc(iface)...)
		for _, ep := range iface.Endpoints {
			fsBlob = append(fsBlob, buildEPDesc(ep)...)
			hsBlob = append(hsBlob, buildEPDesc(ep)...)
		}
		if iface.InterfaceClass == 0x03 && len(iface.ReportDescriptor) > 0 {
			fsBlob = append(fsBlob, buildHIDDesc(iface.ReportDescriptor)...)
			hsBlob = append(hsBlob, buildHIDDesc(iface.ReportDescriptor)...)
		}
	}

	counts.FSCount = countDescriptors(fsBlob)
	counts.HSCount = countDescriptors(hsBlob)

	fsDescs = [][]byte{fsBlob}
	hsDescs = [][]byte{hsBlob}
	return
}

// countDescriptors walks a raw descriptor blob and counts individual descriptors
func countDescriptors(data []byte) int {
	count := 0
	off := 0
	for off < len(data) {
		if off+1 >= len(data) {
			break
		}
		descLen := int(data[off])
		if descLen < 2 {
			break
		}
		count++
		off += descLen
	}
	return count
}

func buildIfaceDesc(i usb.InterfaceDescriptor) []byte {
	b := make([]byte, 9)
	b[0] = 9
	b[1] = 4
	b[2] = i.InterfaceNumber
	b[3] = i.AlternateSetting
	b[4] = i.NumEndpoints
	b[5] = i.InterfaceClass
	b[6] = i.InterfaceSubClass
	b[7] = i.InterfaceProtocol
	b[8] = 0
	return b
}

func buildEPDesc(ep usb.EndpointDescriptor) []byte {
	b := make([]byte, 7)
	b[0] = 7
	b[1] = 5
	b[2] = ep.EndpointAddress
	b[3] = ep.Attributes
	b[4] = byte(ep.MaxPacketSize)
	b[5] = byte(ep.MaxPacketSize >> 8)
	b[6] = ep.Interval
	return b
}

func buildHIDDesc(reportDesc []byte) []byte {
	b := make([]byte, 9)
	b[0] = 9
	b[1] = 0x21
	b[2] = 0x11
	b[3] = 0x01
	b[4] = 0
	b[5] = 1
	b[6] = 0x22
	rlen := len(reportDesc)
	b[7] = byte(rlen)
	b[8] = byte(rlen >> 8)
	return b
}

// buildFFSStrings builds string descriptors for FunctionFS.
// FunctionFS requires string data written to ep0 after descriptors.
func buildFFSStrings(devDesc usb.DeviceDescriptor) []gadget.LangStrings {
	strs := []string{}
	if devDesc.Manufacturer != "" {
		strs = append(strs, devDesc.Manufacturer)
	}
	if devDesc.Product != "" {
		strs = append(strs, devDesc.Product)
	}
	if devDesc.SerialNumber != "" {
		strs = append(strs, devDesc.SerialNumber)
	}
	if len(strs) == 0 {
		return nil
	}
	return []gadget.LangStrings{
		{LangID: 0x0409, Strings: strs}, // English (US)
	}
}
