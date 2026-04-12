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

// DeviceHandle 持久化 USB 设备句柄，用于仿真期间的持续 I/O
type DeviceHandle struct {
	ctx    *C.libusb_context
	handle *C.libusb_device_handle
}

// OpenDevice 通过 bus:dev 地址查找并打开 USB 设备
func OpenDevice(bus, devAddr int) (*DeviceHandle, error) {
	var ctx *C.libusb_context
	rc := C.libusb_init(&ctx)
	if rc != 0 {
		return nil, fmt.Errorf("libusb_init: %s", libusbErrStr(rc))
	}

	var devices **C.libusb_device
	count := C.libusb_get_device_list(ctx, &devices)
	if count < 0 {
		C.libusb_exit(ctx)
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
		C.libusb_exit(ctx)
		return nil, fmt.Errorf("device %d:%d not found", bus, devAddr)
	}

	var handle *C.libusb_device_handle
	rc = C.libusb_open(target, &handle)
	if rc != 0 {
		C.libusb_exit(ctx)
		return nil, fmt.Errorf("libusb_open: %s", libusbErrStr(rc))
	}

	return &DeviceHandle{ctx: ctx, handle: handle}, nil
}

// ClaimInterface 声明 USB 接口
func (d *DeviceHandle) ClaimInterface(iface uint8) error {
	rc := C.libusb_claim_interface(d.handle, C.int(iface))
	if rc != 0 {
		return fmt.Errorf("claim interface %d: %s", iface, libusbErrStr(rc))
	}
	return nil
}

// ReleaseInterface 释放 USB 接口
func (d *DeviceHandle) ReleaseInterface(iface uint8) error {
	rc := C.libusb_release_interface(d.handle, C.int(iface))
	if rc != 0 {
		return fmt.Errorf("release interface %d: %s", iface, libusbErrStr(rc))
	}
	return nil
}

// InterruptRead 执行中断 IN 传输
func (d *DeviceHandle) InterruptRead(endpoint uint8, length int, timeoutMs int) ([]byte, error) {
	buf := make([]byte, length)
	var transferred C.int
	rc := C.libusb_interrupt_transfer(
		d.handle,
		C.uchar(endpoint),
		(*C.uchar)(unsafe.Pointer(&buf[0])),
		C.int(length),
		&transferred,
		C.uint(timeoutMs),
	)
	if rc < 0 {
		if rc == C.LIBUSB_ERROR_TIMEOUT {
			return nil, nil // 超时返回空，不报错
		}
		return nil, fmt.Errorf("interrupt transfer ep 0x%02X: %s", endpoint, libusbErrStr(rc))
	}
	return buf[:int(transferred)], nil
}

// Close 关闭设备并释放 libusb 上下文
func (d *DeviceHandle) Close() {
	if d.handle != nil {
		C.libusb_close(d.handle)
		d.handle = nil
	}
	if d.ctx != nil {
		C.libusb_exit(d.ctx)
		d.ctx = nil
	}
}
