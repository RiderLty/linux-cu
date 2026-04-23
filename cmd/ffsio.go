package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/linux-cu/pkg/gadget"
	"github.com/linux-cu/pkg/pipe"
	"github.com/linux-cu/pkg/usb"
)

// ffsEndpointInfo describes an endpoint for FFS I/O
type ffsEndpointInfo struct {
	InterfaceNumber uint8
	EndpointAddress uint8
	Attributes      uint8 // bmAttributes (transfer type)
	MaxPacketSize   uint16
}

// ffsIO manages I/O for a single FFS function
type ffsIO struct {
	ffs     *gadget.FFSFunction
	epFiles map[uint8]*os.File // endpoint address -> file
	inEPs   []ffsEndpointInfo  // IN endpoints (device -> host)
	outEPs  []ffsEndpointInfo  // OUT endpoints (host -> device)
}

// setupFFS mounts FFS and writes descriptors. Must be called BEFORE ConnectUDC.
// The kernel requires FFS descriptors to be written before the gadget can bind to UDC.
func setupFFS(ffs *gadget.FFSFunction, configs []usb.ConfigDescriptor, devDesc usb.DeviceDescriptor) error {
	// Mount FFS
	log.Printf("[FFS] 挂载 %s ...", ffs.Name)
	if err := ffs.Mount(); err != nil {
		return err
	}

	// Build and write descriptors for non-HID interfaces
	fsDescs, hsDescs, counts, _ := buildNonHIDFFSDescriptors(configs)
	strLangs := buildFFSStrings(devDesc, configs)

	log.Printf("[FFS] 写入描述符 (fs_count=%d, hs_count=%d)...", counts.FSCount, counts.HSCount)
	if err := ffs.WriteDescriptors(fsDescs, hsDescs, counts.FSCount, counts.HSCount, strLangs); err != nil {
		return err
	}

	// Verify FFS is ready by checking ep0 events (non-blocking read)
	ep0 := ffs.EP0()
	if ep0 != nil {
		buf := make([]byte, 512)
		ep0.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, err := ep0.Read(buf)
		if err == nil && n > 0 {
			log.Printf("[FFS] ep0 事件: %x", buf[:n])
		} else {
			log.Printf("[FFS] ep0 无事件 (正常，等待 UDC 绑定)")
		}
		ep0.SetReadDeadline(time.Time{})
	}

	return nil
}

// startFFSIO opens FFS endpoint files and starts bidirectional I/O.
// Must be called AFTER ConnectUDC (endpoints only appear after UDC bind).
func startFFSIO(ctx context.Context, ffs *gadget.FFSFunction, configs []usb.ConfigDescriptor, p *pipe.Pipe, debug bool) {
	io := &ffsIO{
		ffs:     ffs,
		epFiles: make(map[uint8]*os.File),
	}

	// Collect non-HID endpoint info from config
	if len(configs) > 0 {
		for _, iface := range configs[0].Interfaces {
			if iface.InterfaceClass == 0x03 {
				continue // HID handled separately
			}
			for _, ep := range iface.Endpoints {
				epInfo := ffsEndpointInfo{
					InterfaceNumber: iface.InterfaceNumber,
					EndpointAddress: ep.EndpointAddress,
					Attributes:      ep.Attributes,
					MaxPacketSize:   ep.MaxPacketSize,
				}
				if ep.EndpointAddress&0x80 != 0 {
					io.inEPs = append(io.inEPs, epInfo)
				} else {
					io.outEPs = append(io.outEPs, epInfo)
				}
			}
		}
	}

	// Open endpoint files
	for _, ep := range io.inEPs {
		f, err := ffs.OpenEndpoint(ep.EndpointAddress)
		if err != nil {
			log.Printf("[FFS] 打开端点 0x%02X 失败: %v", ep.EndpointAddress, err)
			continue
		}
		io.epFiles[ep.EndpointAddress] = f
		log.Printf("[FFS] 已打开 IN 端点 0x%02X (iface=%d)", ep.EndpointAddress, ep.InterfaceNumber)
	}
	for _, ep := range io.outEPs {
		f, err := ffs.OpenEndpoint(ep.EndpointAddress)
		if err != nil {
			log.Printf("[FFS] 打开端点 0x%02X 失败: %v", ep.EndpointAddress, err)
			continue
		}
		io.epFiles[ep.EndpointAddress] = f
		log.Printf("[FFS] 已打开 OUT 端点 0x%02X (iface=%d)", ep.EndpointAddress, ep.InterfaceNumber)
	}

	// Start FFS IN readers: FFS endpoint -> pipe HostToDevice (host writes to gadget)
	for _, ep := range io.inEPs {
		f, ok := io.epFiles[ep.EndpointAddress]
		if !ok {
			continue
		}
		go readFFSEndpoint(ctx, f, ep, p, debug)
	}

	// Start FFS OUT writer: pipe DeviceToHost -> FFS endpoint (gadget sends to host)
	go writeFFSEndpoints(ctx, io, p, debug)

	// Cleanup on context done
	go func() {
		<-ctx.Done()
		for _, f := range io.epFiles {
			f.Close()
		}
	}()
}

// readFFSEndpoint reads from an FFS IN endpoint and sends data to the pipe
func readFFSEndpoint(ctx context.Context, f *os.File, ep ffsEndpointInfo, p *pipe.Pipe, debug bool) {
	buf := make([]byte, 1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := f.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			if debug {
				log.Printf("[DEBUG][FFS→Pipe] iface=%d ep=0x%02X len=%d data=%x", ep.InterfaceNumber, ep.EndpointAddress, n, data)
			}
			msg := pipe.DataMsg(pipe.HostToDevice, ep.EndpointAddress, ep.InterfaceNumber, data)
			p.SendHostToDevice(ctx, msg)
		}
	}
}

// writeFFSEndpoints reads from pipe and writes to the appropriate FFS OUT endpoint
func writeFFSEndpoints(ctx context.Context, io *ffsIO, p *pipe.Pipe, debug bool) {
	// Build map: interface -> OUT endpoint file
	outMap := make(map[uint8]*os.File)
	for _, ep := range io.outEPs {
		if f, ok := io.epFiles[ep.EndpointAddress]; ok {
			outMap[ep.InterfaceNumber] = f
		}
	}

	for {
		msg, err := p.RecvDeviceToHost(ctx)
		if err != nil {
			return
		}
		if msg.Type != pipe.MsgData {
			continue
		}

		f, ok := outMap[msg.Interface]
		if !ok {
			continue
		}

		if debug {
			log.Printf("[DEBUG][Pipe→FFS] iface=%d len=%d data=%x", msg.Interface, len(msg.Data), msg.Data)
		}
		if _, err := f.Write(msg.Data); err != nil {
			log.Printf("[FFS] 写入端点失败: %v", err)
		}
	}
}
