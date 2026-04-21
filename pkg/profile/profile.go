package profile

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/linux-cu/pkg/gadget"
	"github.com/linux-cu/pkg/usb"
	"gopkg.in/yaml.v3"
)

// DeviceProfile is the YAML-serializable representation of a USB device.
type DeviceProfile struct {
	Device   DeviceDescYAML   `yaml:"device"`
	Configs []ConfigDescYAML  `yaml:"configs"`
}

// DeviceDescYAML is the YAML-serializable device descriptor.
type DeviceDescYAML struct {
	BcdUSB          string `yaml:"bcdUSB"`
	DeviceClass     uint8  `yaml:"bDeviceClass"`
	DeviceSubClass  uint8  `yaml:"bDeviceSubClass"`
	DeviceProtocol  uint8  `yaml:"bDeviceProtocol"`
	MaxPacketSize0  uint8  `yaml:"bMaxPacketSize0"`
	VendorID        string `yaml:"idVendor"`
	ProductID       string `yaml:"idProduct"`
	BcdDevice       string `yaml:"bcdDevice"`
	Manufacturer    string `yaml:"manufacturer,omitempty"`
	Product         string `yaml:"product,omitempty"`
	SerialNumber    string `yaml:"serialNumber,omitempty"`
}

// ConfigDescYAML is the YAML-serializable configuration descriptor.
type ConfigDescYAML struct {
	ConfigValue   uint8              `yaml:"bConfigurationValue"`
	MaxPower      uint8              `yaml:"MaxPower"`
	SelfPowered   bool               `yaml:"selfPowered"`
	RemoteWakeup  bool               `yaml:"remoteWakeup"`
	NumInterfaces uint8              `yaml:"bNumInterfaces"`
	Interfaces    []InterfaceDescYAML `yaml:"interfaces"`
}

// InterfaceDescYAML is the YAML-serializable interface descriptor.
type InterfaceDescYAML struct {
	InterfaceNumber   uint8              `yaml:"bInterfaceNumber"`
	AlternateSetting  uint8              `yaml:"bAlternateSetting"`
	InterfaceClass    uint8              `yaml:"bInterfaceClass"`
	InterfaceSubClass uint8              `yaml:"bInterfaceSubClass"`
	InterfaceProtocol uint8              `yaml:"bInterfaceProtocol"`
	InterfaceString   string             `yaml:"interfaceString,omitempty"`
	Endpoints         []EndpointDescYAML `yaml:"endpoints"`
	ReportDescriptor  string             `yaml:"reportDescriptor,omitempty"` // hex-encoded
}

// EndpointDescYAML is the YAML-serializable endpoint descriptor.
type EndpointDescYAML struct {
	EndpointAddress uint8  `yaml:"bEndpointAddress"`
	Attributes      uint8  `yaml:"bmAttributes"`
	MaxPacketSize   uint16 `yaml:"wMaxPacketSize"`
	Interval        uint8  `yaml:"bInterval"`
}

// FromDescriptors creates a DeviceProfile from USB descriptors.
func FromDescriptors(devDesc usb.DeviceDescriptor, configs []usb.ConfigDescriptor) DeviceProfile {
	p := DeviceProfile{
		Device: DeviceDescYAML{
			BcdUSB:         fmt.Sprintf("0x%04x", devDesc.BcdUSB),
			DeviceClass:    devDesc.DeviceClass,
			DeviceSubClass: devDesc.DeviceSubClass,
			DeviceProtocol: devDesc.DeviceProtocol,
			MaxPacketSize0: devDesc.MaxPacketSize0,
			VendorID:       fmt.Sprintf("0x%04x", devDesc.VendorID),
			ProductID:      fmt.Sprintf("0x%04x", devDesc.ProductID),
			BcdDevice:      fmt.Sprintf("0x%04x", devDesc.BcdDevice),
			Manufacturer:   devDesc.Manufacturer,
			Product:        devDesc.Product,
			SerialNumber:   devDesc.SerialNumber,
		},
	}

	for _, cfg := range configs {
		cfgY := ConfigDescYAML{
			ConfigValue:   cfg.ConfigValue,
			MaxPower:      cfg.MaxPower,
			SelfPowered:   cfg.SelfPowered,
			RemoteWakeup:  cfg.RemoteWakeup,
			NumInterfaces: cfg.NumInterfaces,
		}
		for _, iface := range cfg.Interfaces {
			ifaceY := InterfaceDescYAML{
				InterfaceNumber:   iface.InterfaceNumber,
				AlternateSetting:  iface.AlternateSetting,
				InterfaceClass:    iface.InterfaceClass,
				InterfaceSubClass: iface.InterfaceSubClass,
				InterfaceProtocol: iface.InterfaceProtocol,
				InterfaceString:   iface.InterfaceString,
			}
			if len(iface.ReportDescriptor) > 0 {
				ifaceY.ReportDescriptor = hex.EncodeToString(iface.ReportDescriptor)
			}
			for _, ep := range iface.Endpoints {
				ifaceY.Endpoints = append(ifaceY.Endpoints, EndpointDescYAML{
					EndpointAddress: ep.EndpointAddress,
					Attributes:      ep.Attributes,
					MaxPacketSize:   ep.MaxPacketSize,
					Interval:        ep.Interval,
				})
			}
			cfgY.Interfaces = append(cfgY.Interfaces, ifaceY)
		}
		p.Configs = append(p.Configs, cfgY)
	}

	return p
}

// Save writes the DeviceProfile to a YAML file.
func (p *DeviceProfile) Save(path string) error {
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// Load reads a DeviceProfile from a YAML file.
func Load(path string) (*DeviceProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	var p DeviceProfile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}
	return &p, nil
}

// ToGadgetConfig converts the profile's device descriptor into a gadget.Config.
func (p *DeviceProfile) ToGadgetConfig() gadget.Config {
	d := p.Device
	vid := parseHex16(d.VendorID)
	pid := parseHex16(d.ProductID)

	return gadget.Config{
		VID:            vid,
		PID:            pid,
		Manufacturer:   d.Manufacturer,
		Product:        d.Product,
		SerialNumber:   d.SerialNumber,
		DeviceClass:    d.DeviceClass,
		DeviceSubClass: d.DeviceSubClass,
		DeviceProtocol: d.DeviceProtocol,
		MaxPacketSize0: d.MaxPacketSize0,
		BcdDevice:      parseHex16(d.BcdDevice),
		BcdUSB:         parseHex16(d.BcdUSB),
	}
}

// HIDInterfaces returns the HID interface configurations from the first config.
// Only alt setting 0 is included.
func (p *DeviceProfile) HIDInterfaces() []HIDInterfaceInfo {
	if len(p.Configs) == 0 {
		return nil
	}
	cfg := p.Configs[0]
	var result []HIDInterfaceInfo
	for _, iface := range cfg.Interfaces {
		if iface.InterfaceClass != 0x03 {
			continue
		}
		if iface.AlternateSetting != 0 {
			continue
		}
		reportLen := uint16(64)
		for _, ep := range iface.Endpoints {
			if ep.EndpointAddress&0x80 != 0 {
				reportLen = ep.MaxPacketSize
				break
			}
		}
		var reportDesc []byte
		if iface.ReportDescriptor != "" {
			reportDesc, _ = hex.DecodeString(iface.ReportDescriptor)
		}
		result = append(result, HIDInterfaceInfo{
			InterfaceNumber: iface.InterfaceNumber,
			Protocol:        iface.InterfaceProtocol,
			SubClass:        iface.InterfaceSubClass,
			ReportLen:       reportLen,
			ReportDesc:      reportDesc,
		})
	}
	return result
}

// HIDInterfaceInfo holds parsed HID interface info for gadget creation.
type HIDInterfaceInfo struct {
	InterfaceNumber uint8
	Protocol        uint8
	SubClass        uint8
	ReportLen       uint16
	ReportDesc      []byte
}

// ConfigAttrs returns the first config's MaxPower, SelfPowered, RemoteWakeup.
func (p *DeviceProfile) ConfigAttrs() (maxPower uint8, selfPowered, remoteWakeup bool) {
	if len(p.Configs) == 0 {
		return 100, false, false
	}
	cfg := p.Configs[0]
	return cfg.MaxPower, cfg.SelfPowered, cfg.RemoteWakeup
}

func parseHex16(s string) uint16 {
	var v uint16
	fmt.Sscanf(s, "0x%04x", &v)
	if v == 0 {
		fmt.Sscanf(s, "%04x", &v)
	}
	return v
}
