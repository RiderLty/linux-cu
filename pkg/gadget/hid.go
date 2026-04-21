package gadget

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// HIDFunction manages a ConfigFS HID function instance (usb_f_hid).
// Each HID function creates a /dev/hidgN device for read/write.
type HIDFunction struct {
	Name       string // e.g. "hid.usb0"
	Path       string // ConfigFS function path
	Instance   string // e.g. "usb0"
	DevPath    string // e.g. "/dev/hidg0"
	Index      int    // hidg device index

	Protocol    uint8
	SubClass    uint8
	ReportLen   uint16
	ReportDesc  []byte
}

// HIDConfig holds parameters for creating a HID function
type HIDConfig struct {
	Instance    string // e.g. "usb0"
	Protocol    uint8
	SubClass    uint8
	ReportLen   uint16
	ReportDesc  []byte
}

// NewHIDFunction creates a HID function directory in ConfigFS.
func NewHIDFunction(gadgetPath string, cfg HIDConfig) (*HIDFunction, error) {
	funcName := "hid." + cfg.Instance
	funcPath := filepath.Join(gadgetPath, "functions", funcName)

	log.Printf("[HID] mkdir %s", funcPath)
	if err := os.MkdirAll(funcPath, 0755); err != nil {
		return nil, fmt.Errorf("mkdir function %s: %w", funcName, err)
	}

	// Write protocol
	path := filepath.Join(funcPath, "protocol")
	log.Printf("[HID] write %s = %d", path, cfg.Protocol)
	if err := writeFile(path, fmt.Sprintf("%d", cfg.Protocol)); err != nil {
		return nil, fmt.Errorf("write protocol: %w", err)
	}

	// Write subclass
	path = filepath.Join(funcPath, "subclass")
	log.Printf("[HID] write %s = %d", path, cfg.SubClass)
	if err := writeFile(path, fmt.Sprintf("%d", cfg.SubClass)); err != nil {
		return nil, fmt.Errorf("write subclass: %w", err)
	}

	// Write report_length
	path = filepath.Join(funcPath, "report_length")
	log.Printf("[HID] write %s = %d", path, cfg.ReportLen)
	if err := writeFile(path, fmt.Sprintf("%d", cfg.ReportLen)); err != nil {
		return nil, fmt.Errorf("write report_length: %w", err)
	}

	// Write report_desc (binary, no newline)
	path = filepath.Join(funcPath, "report_desc")
	log.Printf("[HID] write %s (%d bytes)", path, len(cfg.ReportDesc))
	if err := os.WriteFile(path, cfg.ReportDesc, 0644); err != nil {
		return nil, fmt.Errorf("write report_desc: %w", err)
	}

	return &HIDFunction{
		Name:       funcName,
		Path:       funcPath,
		Instance:   cfg.Instance,
		Protocol:   cfg.Protocol,
		SubClass:   cfg.SubClass,
		ReportLen:  cfg.ReportLen,
		ReportDesc: cfg.ReportDesc,
	}, nil
}

// ResolveDevPath verifies the /dev/hidgN device exists.
// The Index and DevPath should already be set by AddHIDFunction.
func (h *HIDFunction) ResolveDevPath() error {
	if h.DevPath == "" {
		h.DevPath = fmt.Sprintf("/dev/hidg%d", h.Index)
	}
	if _, err := os.Stat(h.DevPath); err != nil {
		return fmt.Errorf("stat %s: %w", h.DevPath, err)
	}
	log.Printf("[HID] %s resolved to %s", h.Name, h.DevPath)
	return nil
}

// OpenDev opens the /dev/hidgN device for read/write.
func (h *HIDFunction) OpenDev() (*os.File, error) {
	if h.DevPath == "" {
		if err := h.ResolveDevPath(); err != nil {
			return nil, fmt.Errorf("resolve dev path: %w", err)
		}
	}
	f, err := os.OpenFile(h.DevPath, os.O_RDWR, 0666)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", h.DevPath, err)
	}
	return f, nil
}
