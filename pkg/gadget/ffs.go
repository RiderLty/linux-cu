package gadget

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const (
	ffsMountDir = "/dev/usb-ffs"
)

// LangStrings represents string descriptors for one language
type LangStrings struct {
	LangID    uint16
	Strings   []string // ordered: string 0, 1, 2, ...
}

// FFSFunction manages a FunctionFS function instance
type FFSFunction struct {
	Name    string
	Path    string // ConfigFS function path
	mount   string // mount point path
	mounted bool

	ep0File *os.File
	epFiles map[uint8]*os.File // endpoint address -> file
	mu      sync.Mutex
}

// NewFFSFunction creates a FunctionFS function directory in ConfigFS
func NewFFSFunction(gadgetPath, funcName string) (*FFSFunction, error) {
	funcPath := filepath.Join(gadgetPath, "functions", funcName)
	if err := os.MkdirAll(funcPath, 0755); err != nil {
		return nil, fmt.Errorf("mkdir function: %w", err)
	}
	mountDir := filepath.Join(ffsMountDir, funcName)
	return &FFSFunction{
		Name:    funcName,
		Path:    funcPath,
		mount:   mountDir,
		epFiles: make(map[uint8]*os.File),
	}, nil
}

// Mount creates the mount point and mounts FunctionFS
func (f *FFSFunction) Mount() error {
	if f.mounted {
		return nil
	}
	if err := os.MkdirAll(f.mount, 0777); err != nil {
		return fmt.Errorf("mkdir mount: %w", err)
	}

	// Mount functionfs
	// The device name must match the instance name from ConfigFS function directory.
	// For "ffs.usb0", the instance name is "usb0".
	// mount -t functionfs usb0 /dev/usb-ffs/ffs.usb0
	instanceName := f.Name
	if idx := strings.Index(f.Name, "."); idx >= 0 {
		instanceName = f.Name[idx+1:]
	}
	mountCmd := fmt.Sprintf("mount -t functionfs %s %s", instanceName, f.mount)
	if err := runCommand("sh", "-c", mountCmd); err != nil {
		return fmt.Errorf("mount functionfs: %w", err)
	}
	f.mounted = true
	return nil
}

// Unmount unmounts FunctionFS
func (f *FFSFunction) Unmount() {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Close all endpoint files
	for addr, file := range f.epFiles {
		file.Close()
		delete(f.epFiles, addr)
	}
	if f.ep0File != nil {
		f.ep0File.Close()
		f.ep0File = nil
	}

	if f.mounted {
		runCommand("sh", "-c", fmt.Sprintf("umount %s 2>/dev/null || true", f.mount))
		f.mounted = false
	}
}

// WriteDescriptors writes USB descriptors and strings to ep0 for FunctionFS setup.
// This must be called after Mount() and before opening endpoints.
//
// FunctionFS requires two sequential writes to ep0:
// 1. Descriptors blob (v2 format with magic=3)
// 2. Strings blob (with magic=2)
//
// fsCount/hsCount are the total number of individual descriptors (interface + endpoint + class),
// NOT the number of interface groups.
func (f *FFSFunction) WriteDescriptors(fsDescs, hsDescs [][]byte, fsCount, hsCount int, strLangs []LangStrings) error {
	ep0Path := filepath.Join(f.mount, "ep0")
	file, err := os.OpenFile(ep0Path, os.O_RDWR, 0000)
	if err != nil {
		return fmt.Errorf("open ep0: %w", err)
	}
	f.ep0File = file

	// Step 1: Write descriptors (FFS reads descriptors in FFS_READ_DESCRIPTORS state)
	descData := f.buildDescriptorsBlob(fsDescs, hsDescs, nil, fsCount, hsCount, 0)
	fmt.Printf("[FFS] writing descriptors (%d bytes, fs_count=%d, hs_count=%d)\n", len(descData), fsCount, hsCount)
	if _, err := file.Write(descData); err != nil {
		return fmt.Errorf("write descriptors to ep0: %w", err)
	}

	// Step 2: Write strings (FFS reads strings in FFS_READ_STRINGS state)
	// This must be a separate write() call!
	strData := f.buildStringsBlob(strLangs)
	if len(strData) > 0 {
		fmt.Printf("[FFS] writing strings (%d bytes)\n", len(strData))
		if _, err := file.Write(strData); err != nil {
			return fmt.Errorf("write strings to ep0: %w", err)
		}
	}

	return nil
}

// OpenEndpoint opens an endpoint file for I/O
func (f *FFSFunction) OpenEndpoint(addr uint8) (*os.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	name := endpointName(addr)
	epPath := filepath.Join(f.mount, name)

	file, err := os.OpenFile(epPath, os.O_RDWR, 0000)
	if err != nil {
		return nil, fmt.Errorf("open endpoint %s: %w", name, err)
	}
	f.epFiles[addr] = file
	return file, nil
}

// CloseEndpoint closes an endpoint file
func (f *FFSFunction) CloseEndpoint(addr uint8) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if file, ok := f.epFiles[addr]; ok {
		file.Close()
		delete(f.epFiles, addr)
	}
}

// EP0 returns the ep0 file for control transfers
func (f *FFSFunction) EP0() *os.File {
	return f.ep0File
}

func (f *FFSFunction) buildDescriptorsBlob(fsDescs, hsDescs, ssDescs [][]byte, fsCount, hsCount, ssCount int) []byte {
	const (
		magicV2              = 3 // FUNCTIONFS_DESCRIPTORS_MAGIC_V2
		flagHasFSDesc  uint32 = 1 // FUNCTIONFS_HAS_FS_DESC
		flagHasHSDesc  uint32 = 2 // FUNCTIONFS_HAS_HS_DESC
		flagHasSSDesc  uint32 = 4 // FUNCTIONFS_HAS_SS_DESC
	)

	fsSize := 0
	for _, d := range fsDescs {
		fsSize += len(d)
	}
	hsSize := 0
	for _, d := range hsDescs {
		hsSize += len(d)
	}
	ssSize := 0
	for _, d := range ssDescs {
		ssSize += len(d)
	}

	flags := uint32(0)
	countSize := 0
	if fsCount > 0 {
		flags |= flagHasFSDesc
		countSize += 4
	}
	if hsCount > 0 {
		flags |= flagHasHSDesc
		countSize += 4
	}
	if ssCount > 0 {
		flags |= flagHasSSDesc
		countSize += 4
	}

	headerLen := 12 + countSize
	totalLen := headerLen + fsSize + hsSize + ssSize

	buf := make([]byte, totalLen)
	off := 0

	binary.LittleEndian.PutUint32(buf[off:], magicV2)
	off += 4

	binary.LittleEndian.PutUint32(buf[off:], uint32(totalLen))
	off += 4

	binary.LittleEndian.PutUint32(buf[off:], flags)
	off += 4

	if fsCount > 0 {
		binary.LittleEndian.PutUint32(buf[off:], uint32(fsCount))
		off += 4
	}
	if hsCount > 0 {
		binary.LittleEndian.PutUint32(buf[off:], uint32(hsCount))
		off += 4
	}
	if ssCount > 0 {
		binary.LittleEndian.PutUint32(buf[off:], uint32(ssCount))
		off += 4
	}

	for _, d := range fsDescs {
		copy(buf[off:], d)
		off += len(d)
	}
	for _, d := range hsDescs {
		copy(buf[off:], d)
		off += len(d)
	}
	for _, d := range ssDescs {
		copy(buf[off:], d)
		off += len(d)
	}

	return buf
}

func endpointName(addr uint8) string {
	dir := "in"
	if addr&0x80 == 0 {
		dir = "out"
	}
	num := addr & 0x0F
	return fmt.Sprintf("ep%d%s", num, dir)
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

// buildStringsBlob builds the FunctionFS strings blob.
// Format:
//   __le32 magic = 2 (FUNCTIONFS_STRINGS_MAGIC)
//   __le32 length (total)
//   __le32 str_count (total unique strings across all languages)
//   __le32 lang_count
//   For each language:
//     __le16 lang_id
//     For each string: __le8 length, UTF-16LE chars
func (f *FFSFunction) buildStringsBlob(langs []LangStrings) []byte {
	if len(langs) == 0 {
		return nil
	}

	// Count total strings (max across all languages)
	maxStrCount := 0
	for _, l := range langs {
		if len(l.Strings) > maxStrCount {
			maxStrCount = len(l.Strings)
		}
	}

	// Calculate size
	headerLen := 16 // 4 x uint32
	langDataLen := 0
	for _, l := range langs {
		langDataLen += 2 // lang_id
		for _, s := range l.Strings {
			utf16Len := 0
			for _, r := range s {
				if r <= 0xFFFF {
					utf16Len += 2
				} else {
					utf16Len += 4 // surrogate pair
				}
			}
			langDataLen += 1 + utf16Len // length byte + UTF-16LE data
		}
	}
	totalLen := headerLen + langDataLen

	buf := make([]byte, totalLen)
	off := 0

	binary.LittleEndian.PutUint32(buf[off:], 2) // FUNCTIONFS_STRINGS_MAGIC
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], uint32(totalLen))
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], uint32(maxStrCount))
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], uint32(len(langs)))
	off += 4

	for _, l := range langs {
		binary.LittleEndian.PutUint16(buf[off:], l.LangID)
		off += 2
		for _, s := range l.Strings {
			// Encode string as UTF-16LE
			strBuf := make([]byte, 0, len(s)*2)
			for _, r := range s {
				if r <= 0xFFFF {
					strBuf = append(strBuf, byte(r), byte(r>>8))
				} else {
					// Surrogate pair
					r -= 0x10000
					hi := 0xD800 + uint16(r>>10)
					lo := 0xDC00 + uint16(r&0x3FF)
					strBuf = append(strBuf, byte(hi), byte(hi>>8), byte(lo), byte(lo>>8))
				}
			}
			buf[off] = byte(1 + len(strBuf)/2) // bLength: 1 + number of UTF-16 chars
			off++
			copy(buf[off:], strBuf)
			off += len(strBuf)
		}
	}

	return buf
}
