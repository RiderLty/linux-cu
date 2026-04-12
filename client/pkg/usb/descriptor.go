package usb

/*
#cgo pkg-config: libusb-1.0
#include <libusb-1.0/libusb.h>
#include <stdlib.h>
#include <string.h>

// HID 类常量
#define USB_CLASS_HID 0x03
#define USB_DT_HID    0x21
#define USB_DT_REPORT 0x22

// USB 标准请求
#define USB_REQ_GET_DESCRIPTOR 0x06
#define USB_DT_DEVICE          0x01
#define USB_DT_CONFIG          0x02

// C helper: libusb_config_descriptor.interface 字段在 Go 中是关键字,
// 无法直接访问，通过 C 函数获取指针
static const struct libusb_interface* get_config_interfaces(const struct libusb_config_descriptor *cfg) {
	return cfg->interface;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// DeviceInfo USB 设备基本信息
type DeviceInfo struct {
	BusNumber   int
	DevAddress  int
	VendorID    uint16
	ProductID   uint16
	Manufacturer string
	Product      string
	SerialNumber string
	DeviceClass  uint8
}

// DeviceDescriptor USB 设备描述符
type DeviceDescriptor struct {
	BcdUSB          uint16
	DeviceClass     uint8
	DeviceSubClass  uint8
	DeviceProtocol  uint8
	MaxPacketSize0  uint8
	VendorID        uint16
	ProductID       uint16
	BcdDevice       uint16
	NumConfigs      uint8
	Manufacturer    string
	Product         string
	SerialNumber    string
	Raw             []byte
}

// ConfigDescriptor USB 配置描述符（含接口和端点）
type ConfigDescriptor struct {
	ConfigValue    uint8
	ConfigString   string
	MaxPower       uint8
	SelfPowered    bool
	RemoteWakeup   bool
	NumInterfaces  uint8
	Raw            []byte
	Interfaces     []InterfaceDescriptor
}

// InterfaceDescriptor USB 接口描述符
type InterfaceDescriptor struct {
	InterfaceNumber  uint8
	AlternateSetting uint8
	NumEndpoints     uint8
	InterfaceClass   uint8
	InterfaceSubClass uint8
	InterfaceProtocol uint8
	InterfaceString  string
	Raw              []byte
	Endpoints        []EndpointDescriptor
	ReportDescriptor []byte // HID Report Descriptor（如果有）
}

// EndpointDescriptor USB 端点描述符
type EndpointDescriptor struct {
	EndpointAddress uint8
	Attributes      uint8
	MaxPacketSize   uint16
	Interval        uint8
	Raw             []byte
}

// ListDevices 枚举所有 USB 设备
func ListDevices() ([]DeviceInfo, error) {
	var ctx *C.libusb_context
	rc := C.libusb_init(&ctx)
	if rc != 0 {
		return nil, fmt.Errorf("libusb_init failed: %s", libusbErrStr(rc))
	}
	defer C.libusb_exit(ctx)

	var devices **C.libusb_device
	count := C.libusb_get_device_list(ctx, &devices)
	if count < 0 {
		return nil, fmt.Errorf("libusb_get_device_list failed: %s", libusbErrStr(C.int(count)))
	}
	defer C.libusb_free_device_list(devices, 1)

	var result []DeviceInfo
	for i := 0; i < int(count); i++ {
		dev := *(**C.libusb_device)(unsafe.Pointer(uintptr(unsafe.Pointer(devices)) + uintptr(i)*unsafe.Sizeof(*devices)))

		var desc C.struct_libusb_device_descriptor
		rc = C.libusb_get_device_descriptor(dev, &desc)
		if rc != 0 {
			continue
		}

		info := DeviceInfo{
			BusNumber:  int(C.libusb_get_bus_number(dev)),
			DevAddress: int(C.libusb_get_device_address(dev)),
			VendorID:   uint16(desc.idVendor),
			ProductID:  uint16(desc.idProduct),
			DeviceClass: uint8(desc.bDeviceClass),
		}

		// 尝试打开设备读取字符串描述符
		var handle *C.libusb_device_handle
		rc = C.libusb_open(dev, &handle)
		if rc == 0 {
			if desc.iManufacturer != 0 {
				info.Manufacturer = getStringDescriptor(handle, desc.iManufacturer)
			}
			if desc.iProduct != 0 {
				info.Product = getStringDescriptor(handle, desc.iProduct)
			}
			if desc.iSerialNumber != 0 {
				info.SerialNumber = getStringDescriptor(handle, desc.iSerialNumber)
			}
			C.libusb_close(handle)
		}

		result = append(result, info)
	}

	return result, nil
}

// ReadDescriptors 读取指定设备的完整描述符
// bus: USB 总线号, dev: 设备地址
func ReadDescriptors(bus, devAddr int) ([]ConfigDescriptor, DeviceDescriptor, error) {
	var ctx *C.libusb_context
	rc := C.libusb_init(&ctx)
	if rc != 0 {
		return nil, DeviceDescriptor{}, fmt.Errorf("libusb_init failed: %s", libusbErrStr(rc))
	}
	defer C.libusb_exit(ctx)

	var devices **C.libusb_device
	count := C.libusb_get_device_list(ctx, &devices)
	if count < 0 {
		return nil, DeviceDescriptor{}, fmt.Errorf("libusb_get_device_list failed")
	}
	defer C.libusb_free_device_list(devices, 1)

	// 查找目标设备
	var targetDev *C.libusb_device
	for i := 0; i < int(count); i++ {
		dev := *(**C.libusb_device)(unsafe.Pointer(uintptr(unsafe.Pointer(devices)) + uintptr(i)*unsafe.Sizeof(*devices)))
		if int(C.libusb_get_bus_number(dev)) == bus && int(C.libusb_get_device_address(dev)) == devAddr {
			targetDev = dev
			break
		}
	}
	if targetDev == nil {
		return nil, DeviceDescriptor{}, fmt.Errorf("device %d:%d not found", bus, devAddr)
	}

	// 打开设备
	var handle *C.libusb_device_handle
	rc = C.libusb_open(targetDev, &handle)
	if rc != 0 {
		return nil, DeviceDescriptor{}, fmt.Errorf("libusb_open failed: %s", libusbErrStr(rc))
	}
	defer C.libusb_close(handle)

	// 读取设备描述符
	var devDesc C.struct_libusb_device_descriptor
	rc = C.libusb_get_device_descriptor(targetDev, &devDesc)
	if rc != 0 {
		return nil, DeviceDescriptor{}, fmt.Errorf("get device descriptor failed: %s", libusbErrStr(rc))
	}

	dd := parseDeviceDescriptor(handle, &devDesc)

	// 读取所有配置描述符
	var configs []ConfigDescriptor
	for cfgIdx := 0; cfgIdx < int(devDesc.bNumConfigurations); cfgIdx++ {
		var cfgDesc *C.struct_libusb_config_descriptor
		rc = C.libusb_get_config_descriptor(targetDev, C.uint8_t(cfgIdx), &cfgDesc)
		if rc != 0 {
			continue
		}
		cfg := parseConfigDescriptor(handle, cfgDesc)
		configs = append(configs, cfg)
		C.libusb_free_config_descriptor(cfgDesc)
	}

	return configs, dd, nil
}

func parseDeviceDescriptor(handle *C.libusb_device_handle, desc *C.struct_libusb_device_descriptor) DeviceDescriptor {
	dd := DeviceDescriptor{
		BcdUSB:         uint16(desc.bcdUSB),
		DeviceClass:    uint8(desc.bDeviceClass),
		DeviceSubClass: uint8(desc.bDeviceSubClass),
		DeviceProtocol: uint8(desc.bDeviceProtocol),
		MaxPacketSize0: uint8(desc.bMaxPacketSize0),
		VendorID:       uint16(desc.idVendor),
		ProductID:      uint16(desc.idProduct),
		BcdDevice:      uint16(desc.bcdDevice),
		NumConfigs:     uint8(desc.bNumConfigurations),
	}

	if desc.iManufacturer != 0 {
		dd.Manufacturer = getStringDescriptor(handle, desc.iManufacturer)
	}
	if desc.iProduct != 0 {
		dd.Product = getStringDescriptor(handle, desc.iProduct)
	}
	if desc.iSerialNumber != 0 {
		dd.SerialNumber = getStringDescriptor(handle, desc.iSerialNumber)
	}

	// 读取原始设备描述符 (18 bytes)
	dd.Raw = make([]byte, 18)
	buf := (*C.uchar)(unsafe.Pointer(&dd.Raw[0]))
	// 使用控制传输获取原始设备描述符
	C.libusb_control_transfer(
		handle,
		C.uint8_t(0x80),               // bmRequestType: Device-to-Host
		C.uint8_t(C.USB_REQ_GET_DESCRIPTOR), // bRequest
		C.uint16_t(C.USB_DT_DEVICE<<8),      // wValue: Device Descriptor
		C.uint16_t(0),                       // wIndex
		buf,
		C.uint16_t(18),
		C.uint(1000),
	)

	return dd
}

func parseConfigDescriptor(handle *C.libusb_device_handle, desc *C.struct_libusb_config_descriptor) ConfigDescriptor {
	cd := ConfigDescriptor{
		ConfigValue:   uint8(desc.bConfigurationValue),
		MaxPower:      uint8(desc.MaxPower),
		SelfPowered:   (desc.bmAttributes & 0x40) != 0,
		RemoteWakeup:  (desc.bmAttributes & 0x20) != 0,
		NumInterfaces: uint8(desc.bNumInterfaces),
	}

	if desc.iConfiguration != 0 {
		cd.ConfigString = getStringDescriptor(handle, desc.iConfiguration)
	}

	// 读取原始配置描述符
	totalLen := int(desc.wTotalLength)
	cd.Raw = make([]byte, totalLen)
	buf := (*C.uchar)(unsafe.Pointer(&cd.Raw[0]))
	C.libusb_control_transfer(
		handle,
		C.uint8_t(0x80),
		C.uint8_t(C.USB_REQ_GET_DESCRIPTOR),
		C.uint16_t(C.USB_DT_CONFIG<<8),
		C.uint16_t(0),
		buf,
		C.uint16_t(totalLen),
		C.uint(1000),
	)

	// 解析接口描述符
	interfaces := (*[256]C.struct_libusb_interface)(unsafe.Pointer(C.get_config_interfaces(desc)))[:desc.bNumInterfaces:desc.bNumInterfaces]
	for _, iface := range interfaces {
		altSettings := (*[256]C.struct_libusb_interface_descriptor)(unsafe.Pointer(iface.altsetting))[:iface.num_altsetting:iface.num_altsetting]
		for _, altDesc := range altSettings {
			id := InterfaceDescriptor{
				InterfaceNumber:   uint8(altDesc.bInterfaceNumber),
				AlternateSetting:  uint8(altDesc.bAlternateSetting),
				NumEndpoints:      uint8(altDesc.bNumEndpoints),
				InterfaceClass:    uint8(altDesc.bInterfaceClass),
				InterfaceSubClass: uint8(altDesc.bInterfaceSubClass),
				InterfaceProtocol: uint8(altDesc.bInterfaceProtocol),
			}

			if altDesc.iInterface != 0 {
				id.InterfaceString = getStringDescriptor(handle, altDesc.iInterface)
			}

			// 解析端点描述符
			if altDesc.endpoint != nil && altDesc.bNumEndpoints > 0 {
				endpoints := (*[256]C.struct_libusb_endpoint_descriptor)(unsafe.Pointer(altDesc.endpoint))[:altDesc.bNumEndpoints:altDesc.bNumEndpoints]
				for _, epDesc := range endpoints {
					ep := EndpointDescriptor{
						EndpointAddress: uint8(epDesc.bEndpointAddress),
						Attributes:      uint8(epDesc.bmAttributes),
						MaxPacketSize:   uint16(epDesc.wMaxPacketSize),
						Interval:        uint8(epDesc.bInterval),
					}
					if epDesc.extra != nil && epDesc.extra_length > 0 {
						ep.Raw = C.GoBytes(unsafe.Pointer(epDesc.extra), C.int(epDesc.extra_length))
					}
					id.Endpoints = append(id.Endpoints, ep)
				}
			}

			// 读取 HID Report Descriptor
			if altDesc.bInterfaceClass == C.USB_CLASS_HID && altDesc.extra != nil {
				// 解析 HID 描述符获取 Report Descriptor 长度
				extraLen := int(altDesc.extra_length)
				extra := C.GoBytes(unsafe.Pointer(altDesc.extra), C.int(extraLen))
				reportLen := parseHIDReportDescriptorLength(extra)
				if reportLen > 0 {
					reportBuf := make([]byte, reportLen)
					rbuf := (*C.uchar)(unsafe.Pointer(&reportBuf[0]))
					C.libusb_control_transfer(
						handle,
						C.uint8_t(0x81),                  // bmRequestType: Device-to-Host, Interface
						C.uint8_t(C.USB_REQ_GET_DESCRIPTOR),  // bRequest
						C.uint16_t(C.USB_DT_REPORT<<8),       // wValue: HID Report Descriptor
						C.uint16_t(altDesc.bInterfaceNumber), // wIndex: Interface number
						rbuf,
						C.uint16_t(reportLen),
						C.uint(1000),
					)
					id.ReportDescriptor = reportBuf
				}
			}

			cd.Interfaces = append(cd.Interfaces, id)
		}
	}

	return cd
}

// parseHIDReportDescriptorLength 从 HID 描述符 extra 中提取 Report Descriptor 长度
// HID 描述符格式: bLength, bDescriptorType(0x21), bcdHID, bCountryCode, bNumDescriptors,
//                  bDescriptorType2, wDescriptorLength(LE)
func parseHIDReportDescriptorLength(extra []byte) int {
	if len(extra) < 9 {
		return 0
	}
	// wDescriptorLength 在 offset 7-8 (小端序)
	return int(extra[7]) | (int(extra[8]) << 8)
}

func getStringDescriptor(handle *C.libusb_device_handle, index C.uint8_t) string {
	var buf [256]C.uchar
	rc := C.libusb_get_string_descriptor_ascii(handle, C.uint8_t(index), &buf[0], 256)
	if rc <= 0 {
		return ""
	}
	return C.GoStringN((*C.char)(unsafe.Pointer(&buf[0])), rc)
}

func libusbErrStr(code C.int) string {
	return C.GoString(C.libusb_strerror(code))
}
