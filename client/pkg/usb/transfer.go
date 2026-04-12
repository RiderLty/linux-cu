package usb

/*
#cgo pkg-config: libusb-1.0
#include <libusb-1.0/libusb.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// CtrlTransferResult 控制传输结果
type CtrlTransferResult struct {
	Data  []byte // IN 传输的响应数据
	Stall bool   // 设备是否 STALL
}

// ProxyCtrlTransfer 在真实 USB 设备上执行控制传输
// 每次调用独立开关 libusb 上下文和设备句柄
func ProxyCtrlTransfer(bus, devAddr int, bmRequestType, bRequest uint8, wValue, wIndex, wLength uint16, dataPhase []byte) (*CtrlTransferResult, error) {
	var ctx *C.libusb_context
	rc := C.libusb_init(&ctx)
	if rc != 0 {
		return nil, fmt.Errorf("libusb_init: %s", libusbErrStr(rc))
	}
	defer C.libusb_exit(ctx)

	// 查找设备
	var devices **C.libusb_device
	count := C.libusb_get_device_list(ctx, &devices)
	if count < 0 {
		return nil, fmt.Errorf("libusb_get_device_list failed")
	}
	defer C.libusb_free_device_list(devices, 1)

	var target *C.libusb_device
	for i := 0; i < int(count); i++ {
		dev := *(**C.libusb_device)(unsafe.Pointer(uintptr(unsafe.Pointer(devices)) + uintptr(i)*unsafe.Sizeof(*devices)))
		if int(C.libusb_get_bus_number(dev)) == bus && int(C.libusb_get_device_address(dev)) == devAddr {
			target = dev
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("device %d:%d not found", bus, devAddr)
	}

	// 打开设备
	var handle *C.libusb_device_handle
	rc = C.libusb_open(target, &handle)
	if rc != 0 {
		return nil, fmt.Errorf("libusb_open: %s", libusbErrStr(rc))
	}
	defer C.libusb_close(handle)

	isIn := (bmRequestType & 0x80) != 0

	if isIn {
		// IN 传输：读取响应数据
		buf := make([]byte, wLength)
		cbuf := (*C.uchar)(unsafe.Pointer(&buf[0]))
		ret := C.libusb_control_transfer(
			handle,
			C.uint8_t(bmRequestType),
			C.uint8_t(bRequest),
			C.uint16_t(wValue),
			C.uint16_t(wIndex),
			cbuf,
			C.uint16_t(wLength),
			C.uint(1000),
		)
		if ret < 0 {
			if ret == C.LIBUSB_ERROR_PIPE {
				return &CtrlTransferResult{Stall: true}, nil
			}
			return nil, fmt.Errorf("control transfer: %s", libusbErrStr(C.int(ret)))
		}
		return &CtrlTransferResult{Data: buf[:ret]}, nil
	}

	// OUT 传输：发送数据
	var cbuf *C.uchar
	if len(dataPhase) > 0 {
		cbuf = (*C.uchar)(unsafe.Pointer(&dataPhase[0]))
	}
	ret := C.libusb_control_transfer(
		handle,
		C.uint8_t(bmRequestType),
		C.uint8_t(bRequest),
		C.uint16_t(wValue),
		C.uint16_t(wIndex),
		cbuf,
		C.uint16_t(len(dataPhase)),
		C.uint(1000),
	)
	if ret < 0 {
		if ret == C.LIBUSB_ERROR_PIPE {
			return &CtrlTransferResult{Stall: true}, nil
		}
		return nil, fmt.Errorf("control transfer: %s", libusbErrStr(C.int(ret)))
	}

	return &CtrlTransferResult{}, nil
}
