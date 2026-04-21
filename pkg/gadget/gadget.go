package gadget

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const (
	configFSPath  = "/sys/kernel/config/usb_gadget"
	udcClassPath  = "/sys/class/udc"
	gadgetNameFmt = "linux_cu_%04x_%04x"
)

// Gadget represents a USB gadget device managed via ConfigFS
type Gadget struct {
	Name         string
	Path         string
	VID          uint16
	PID          uint16
	UDC          string
	hidFuncs     []*HIDFunction
	IfaceToHidIdx map[uint8]int // maps original InterfaceNumber -> HID function index
}

// Config holds parameters for creating a gadget
type Config struct {
	VID            uint16
	PID            uint16
	Manufacturer   string
	Product        string
	SerialNumber   string
	DeviceClass    uint8
	DeviceSubClass uint8
	DeviceProtocol uint8
	MaxPacketSize0 uint8
	BcdDevice      uint16
	BcdUSB         uint16
	MaxPower       uint8 // in 2mA units
	SelfPowered    bool
	RemoteWakeup   bool
}

// Create creates a new USB gadget device via ConfigFS.
// If a gadget with the same VID:PID already exists, it is destroyed first.
func Create(cfg Config) (*Gadget, error) {
	name := fmt.Sprintf(gadgetNameFmt, cfg.VID, cfg.PID)
	gPath := filepath.Join(configFSPath, name)

	// Destroy existing if present
	if _, err := os.Stat(gPath); err == nil {
		if err := Destroy(name); err != nil {
			return nil, fmt.Errorf("destroy existing gadget: %w", err)
		}
	}

	g := &Gadget{Name: name, Path: gPath, VID: cfg.VID, PID: cfg.PID}

	// Create gadget directory
	log.Printf("[Gadget] mkdir %s", gPath)
	if err := os.MkdirAll(gPath, 0755); err != nil {
		return nil, fmt.Errorf("mkdir gadget: %w", err)
	}

	// Write device descriptors
	writes := []struct {
		file   string
		val    string
		must   bool // if true, error is fatal; if false, warn and continue
	}{
		{"idVendor", fmt.Sprintf("0x%04x", cfg.VID), true},
		{"idProduct", fmt.Sprintf("0x%04x", cfg.PID), true},
		{"bcdDevice", fmt.Sprintf("0x%04x", cfg.BcdDevice), false},
		{"bcdUSB", fmt.Sprintf("0x%04x", cfg.BcdUSB), true},
		{"bDeviceClass", fmt.Sprintf("0x%02x", cfg.DeviceClass), true},
		{"bDeviceSubClass", fmt.Sprintf("0x%02x", cfg.DeviceSubClass), true},
		{"bDeviceProtocol", fmt.Sprintf("0x%02x", cfg.DeviceProtocol), true},
		{"bMaxPacketSize0", fmt.Sprintf("%d", cfg.MaxPacketSize0), true},
	}
	for _, w := range writes {
		path := filepath.Join(gPath, w.file)
		log.Printf("[Gadget] write %s = %s", path, w.val)
		if err := writeFile(path, w.val); err != nil {
			if w.must {
				g.cleanup()
				return nil, fmt.Errorf("write %s: %w", w.file, err)
			}
			log.Printf("[Gadget] 警告: write %s 失败 (非关键): %v", w.file, err)
		}
	}

	// Strings
	stringsDir := filepath.Join(gPath, "strings", "0x409")
	log.Printf("[Gadget] mkdir %s", stringsDir)
	if err := os.MkdirAll(stringsDir, 0755); err != nil {
		g.cleanup()
		return nil, err
	}
	if cfg.Manufacturer != "" {
		p := filepath.Join(stringsDir, "manufacturer")
		log.Printf("[Gadget] write %s = %s", p, cfg.Manufacturer)
		writeFile(p, cfg.Manufacturer)
	}
	if cfg.Product != "" {
		p := filepath.Join(stringsDir, "product")
		log.Printf("[Gadget] write %s = %s", p, cfg.Product)
		writeFile(p, cfg.Product)
	}
	if cfg.SerialNumber != "" {
		p := filepath.Join(stringsDir, "serialnumber")
		log.Printf("[Gadget] write %s = %s", p, cfg.SerialNumber)
		writeFile(p, cfg.SerialNumber)
	}

	// Configuration c.1
	cfgDir := filepath.Join(gPath, "configs", "c.1")
	log.Printf("[Gadget] mkdir %s", cfgDir)
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		g.cleanup()
		return nil, err
	}
	cfgStringsDir := filepath.Join(cfgDir, "strings", "0x409")
	log.Printf("[Gadget] mkdir %s", cfgStringsDir)
	if err := os.MkdirAll(cfgStringsDir, 0755); err != nil {
		g.cleanup()
		return nil, err
	}

	bmAttrs := uint8(0x80)
	if cfg.SelfPowered {
		bmAttrs |= 0x40
	}
	if cfg.RemoteWakeup {
		bmAttrs |= 0x20
	}
	log.Printf("[Gadget] write %s/bmAttributes = 0x%02x", cfgDir, bmAttrs)
	writeFile(filepath.Join(cfgDir, "bmAttributes"), fmt.Sprintf("0x%02x", bmAttrs))
	log.Printf("[Gadget] write %s/MaxPower = %d", cfgDir, cfg.MaxPower)
	writeFile(filepath.Join(cfgDir, "MaxPower"), fmt.Sprintf("%d", cfg.MaxPower))

	return g, nil
}

// AddHIDFunction creates a HID function and links it into the configuration.
// Each HID interface from the source device should be added as a separate HID function.
// ifaceNum is the original USB interface number from the source device.
func (g *Gadget) AddHIDFunction(ifaceNum uint8, cfg HIDConfig) error {
	hid, err := NewHIDFunction(g.Path, cfg)
	if err != nil {
		return fmt.Errorf("create hid function: %w", err)
	}

	// Link function into configuration
	cfgDir := filepath.Join(g.Path, "configs", "c.1")
	linkPath := filepath.Join(cfgDir, hid.Name)
	log.Printf("[Gadget] ln -s %s %s", hid.Path, linkPath)
	if err := os.Symlink(hid.Path, linkPath); err != nil {
		return fmt.Errorf("link hid function: %w", err)
	}

	// Assign hidg index based on creation order
	hid.Index = len(g.hidFuncs)
	hid.DevPath = fmt.Sprintf("/dev/hidg%d", hid.Index)
	log.Printf("[Gadget] %s -> %s (iface %d -> hidIdx %d)", hid.Name, hid.DevPath, ifaceNum, hid.Index)

	if g.IfaceToHidIdx == nil {
		g.IfaceToHidIdx = make(map[uint8]int)
	}
	g.IfaceToHidIdx[ifaceNum] = hid.Index
	g.hidFuncs = append(g.hidFuncs, hid)
	return nil
}

// HIDFunctions returns the list of HID functions
func (g *Gadget) HIDFunctions() []*HIDFunction {
	return g.hidFuncs
}

// ConnectUDC connects the gadget to UDC (makes it visible to the host)
func (g *Gadget) ConnectUDC() error {
	udc, err := getFirstUDC()
	if err != nil {
		return fmt.Errorf("find UDC: %w", err)
	}
	g.UDC = udc

	log.Printf("[Gadget] write %s/UDC = %s", g.Path, udc)
	if err := writeFile(filepath.Join(g.Path, "UDC"), udc); err != nil {
		return fmt.Errorf("write UDC: %w", err)
	}
	return nil
}

// Disconnect disconnects from UDC
func (g *Gadget) Disconnect() error {
	if g.UDC == "" {
		return nil
	}
	writeFile(filepath.Join(g.Path, "UDC"), "")
	g.UDC = ""
	return nil
}

// Destroy removes the gadget completely
func (g *Gadget) Destroy() error {
	if g == nil {
		return nil
	}
	g.Disconnect()
	return g.cleanup()
}

// Destroy removes a named gadget from ConfigFS
func Destroy(name string) error {
	gPath := filepath.Join(configFSPath, name)
	_ = writeFile(filepath.Join(gPath, "UDC"), "")

	// Remove symlinks from configs
	configsDir := filepath.Join(gPath, "configs")
	if entries, err := os.ReadDir(configsDir); err == nil {
		for _, cfg := range entries {
			cfgPath := filepath.Join(configsDir, cfg.Name())
			if links, err := os.ReadDir(cfgPath); err == nil {
				for _, l := range links {
					lp := filepath.Join(cfgPath, l.Name())
					if l.Type() == os.ModeSymlink {
						os.Remove(lp)
					}
				}
			}
			os.RemoveAll(filepath.Join(cfgPath, "strings"))
		}
	}
	os.RemoveAll(filepath.Join(gPath, "functions"))
	os.RemoveAll(filepath.Join(gPath, "strings"))
	os.RemoveAll(gPath)
	return nil
}

func (g *Gadget) cleanup() error {
	return Destroy(g.Name)
}

func getFirstUDC() (string, error) {
	entries, err := os.ReadDir(udcClassPath)
	if err != nil {
		return "", fmt.Errorf("read UDC class: %w", err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("no UDC available")
	}
	return entries[0].Name(), nil
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content+"\n"), 0644)
}
