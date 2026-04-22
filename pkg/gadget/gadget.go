package gadget

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	configFSPath  = "/sys/kernel/config/usb_gadget"
	udcClassPath  = "/sys/class/udc"
	gadgetNameFmt = "linux_cu_%04x_%04x"
)

// Gadget represents a USB gadget device managed via ConfigFS
type Gadget struct {
	Name           string
	Path           string
	VID            uint16
	PID            uint16
	UDC            string
	hidFuncs       []*HIDFunction
	ffsFuncs       []*FFSFunction
	IfaceToHidIdx  map[uint8]int  // maps original InterfaceNumber -> HID function index
	IfaceToFFSIdx  map[uint8]int  // maps original InterfaceNumber -> FFS function index
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
// Any other linux_cu_* gadgets are also cleaned up to free the UDC.
func Create(cfg Config) (*Gadget, error) {
	// Clean up all leftover gadgets first to ensure UDC is free
	CleanupAll()

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

// AddFFSFunction creates a FunctionFS function and links it into the configuration.
// ifaceNums are the original USB interface numbers handled by this FFS instance.
func (g *Gadget) AddFFSFunction(funcName string, ifaceNums []uint8) (*FFSFunction, error) {
	ffs, err := NewFFSFunction(g.Path, funcName)
	if err != nil {
		return nil, fmt.Errorf("create ffs function: %w", err)
	}

	// Link function into configuration
	cfgDir := filepath.Join(g.Path, "configs", "c.1")
	linkPath := filepath.Join(cfgDir, ffs.Name)
	log.Printf("[Gadget] ln -s %s %s", ffs.Path, linkPath)
	if err := os.Symlink(ffs.Path, linkPath); err != nil {
		return nil, fmt.Errorf("link ffs function: %w", err)
	}

	idx := len(g.ffsFuncs)
	log.Printf("[Gadget] %s -> FFS#%d (ifaces %v)", ffs.Name, idx, ifaceNums)

	if g.IfaceToFFSIdx == nil {
		g.IfaceToFFSIdx = make(map[uint8]int)
	}
	for _, n := range ifaceNums {
		g.IfaceToFFSIdx[n] = idx
	}
	g.ffsFuncs = append(g.ffsFuncs, ffs)
	return ffs, nil
}

// FFSFunctions returns the list of FFS functions
func (g *Gadget) FFSFunctions() []*FFSFunction {
	return g.ffsFuncs
}

// ConnectUDC connects the gadget to UDC (makes it visible to the host)
func (g *Gadget) ConnectUDC() error {
	udc, err := getFirstUDC()
	if err != nil {
		return fmt.Errorf("find UDC: %w", err)
	}

	// Free UDC if it's already bound to another gadget
	if err := freeUDC(udc); err != nil {
		log.Printf("[Gadget] 警告: 释放 UDC 失败: %v", err)
	}

	g.UDC = udc
	log.Printf("[Gadget] write %s/UDC = %s", g.Path, udc)

	// Retry UDC write - the UDC may need a moment to be fully released
	var lastErr error
	for i := 0; i < 5; i++ {
		if err := writeFile(filepath.Join(g.Path, "UDC"), udc); err != nil {
			lastErr = err
			log.Printf("[Gadget] UDC 写入失败 (尝试 %d/5): %v", i+1, err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return nil
	}
	return fmt.Errorf("write UDC after retries: %w", lastErr)
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
	// Unmount FFS functions first
	for _, ffs := range g.ffsFuncs {
		ffs.Unmount()
	}
	g.Disconnect()
	return g.cleanup()
}

// Destroy removes a named gadget from ConfigFS
func Destroy(name string) error {
	gPath := filepath.Join(configFSPath, name)

	// Disconnect UDC first
	_ = writeFile(filepath.Join(gPath, "UDC"), "")

	// Unmount any FFS mounts associated with this gadget
	unmountFFSForGadget(gPath)

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

// CleanupAll removes all linux_cu_* gadgets and frees the UDC
func CleanupAll() error {
	entries, err := os.ReadDir(configFSPath)
	if err != nil {
		return fmt.Errorf("read configfs: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if len(name) > len("linux_cu_") && name[:len("linux_cu_")] == "linux_cu_" {
			log.Printf("[Gadget] 清理残留 gadget: %s", name)
			if err := Destroy(name); err != nil {
				log.Printf("[Gadget] 清理 %s 失败: %v", name, err)
			}
		}
	}
	return nil
}

// freeUDC disconnects any gadget currently using the specified UDC
func freeUDC(udcName string) error {
	entries, err := os.ReadDir(configFSPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		udcPath := filepath.Join(configFSPath, e.Name(), "UDC")
		data, err := os.ReadFile(udcPath)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == udcName {
			log.Printf("[Gadget] 释放 UDC: 断开 %s", e.Name())
			_ = writeFile(udcPath, "")
		}
	}
	return nil
}

// unmountFFSForGadget unmounts any FFS filesystems associated with a gadget
func unmountFFSForGadget(gadgetPath string) {
	funcsDir := filepath.Join(gadgetPath, "functions")
	entries, err := os.ReadDir(funcsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if len(name) > 4 && name[:4] == "ffs." {
			mountPoint := filepath.Join(ffsMountDir, name)
			log.Printf("[Gadget] 卸载 FFS: %s", mountPoint)
			runCommand("sh", "-c", fmt.Sprintf("umount %s 2>/dev/null || true", mountPoint))
		}
	}
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
