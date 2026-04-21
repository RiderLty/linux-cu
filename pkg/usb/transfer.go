package usb

import "fmt"

// FindDeviceByVIDPID finds a USB device by VID:PID and returns bus:dev
func FindDeviceByVIDPID(vid, pid uint16) (int, int, error) {
	devices, err := ListDevices()
	if err != nil {
		return 0, 0, err
	}
	for _, d := range devices {
		if d.VendorID == vid && d.ProductID == pid {
			return d.BusNumber, d.DevAddress, nil
		}
	}
	return 0, 0, fmt.Errorf("device VID:PID=%04x:%04x not found", vid, pid)
}
