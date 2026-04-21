package pipe

import (
	"context"
	"sync"
)

// Direction indicates data flow direction
type Direction uint8

const (
	HostToDevice Direction = iota // Host → Device (OUT from host perspective)
	DeviceToHost                  // Device → Host (IN from host perspective)
)

// MsgType classifies the pipe message
type MsgType uint8

const (
	MsgData    MsgType = iota // USB data transfer (interrupt/bulk/isochronous)
	MsgControl               // USB control transfer request/response
	MsgEvent                 // Lifecycle event (connect/disconnect/error)
)

// PipeMsg is the unified message format for all USB data and events
type PipeMsg struct {
	Direction Direction
	Type      MsgType
	Endpoint  uint8 // USB endpoint address
	Interface uint8 // USB interface number
	Data      []byte
	// Control-specific fields (used when Type == MsgControl)
	BMRequestType uint8
	BRequest      uint8
	WValue        uint16
	WIndex        uint16
	WLength       uint16
	Stall         bool // true if device STALLed
}

// Pipe provides a pair of bidirectional channels for USB data transport
type Pipe struct {
	HostToDevice chan PipeMsg // messages flowing host → device
	DeviceToHost chan PipeMsg // messages flowing device → host
	mu          sync.Mutex
	closed      bool
	cancel      context.CancelFunc
}

// New creates a new Pipe with the given buffer size
func New(bufSize int) *Pipe {
	return &Pipe{
		HostToDevice: make(chan PipeMsg, bufSize),
		DeviceToHost: make(chan PipeMsg, bufSize),
	}
}

// NewWithContext creates a Pipe tied to a context for cancellation
func NewWithContext(ctx context.Context, bufSize int) (*Pipe, context.Context) {
	childCtx, cancel := context.WithCancel(ctx)
	p := &Pipe{
		HostToDevice: make(chan PipeMsg, bufSize),
		DeviceToHost: make(chan PipeMsg, bufSize),
		cancel:       cancel,
	}
	return p, childCtx
}

// SendHostToDevice sends a message in the host→device direction
func (p *Pipe) SendHostToDevice(ctx context.Context, msg PipeMsg) error {
	select {
	case p.HostToDevice <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendDeviceToHost sends a message in the device→host direction
func (p *Pipe) SendDeviceToHost(ctx context.Context, msg PipeMsg) error {
	select {
	case p.DeviceToHost <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RecvHostToDevice receives a message from the host→device channel
func (p *Pipe) RecvHostToDevice(ctx context.Context) (PipeMsg, error) {
	select {
	case msg := <-p.HostToDevice:
		return msg, nil
	case <-ctx.Done():
		return PipeMsg{}, ctx.Err()
	}
}

// RecvDeviceToHost receives a message from the device→host channel
func (p *Pipe) RecvDeviceToHost(ctx context.Context) (PipeMsg, error) {
	select {
	case msg := <-p.DeviceToHost:
		return msg, nil
	case <-ctx.Done():
		return PipeMsg{}, ctx.Err()
	}
}

// Close closes both channels
func (p *Pipe) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.cancel != nil {
		p.cancel()
	}
}

// DataMsg creates a data PipeMsg
func DataMsg(dir Direction, ep uint8, iface uint8, data []byte) PipeMsg {
	return PipeMsg{
		Direction: dir,
		Type:      MsgData,
		Endpoint:  ep,
		Interface: iface,
		Data:      data,
	}
}

// CtrlReqMsg creates a control request PipeMsg (device→host, gadget received a request from host)
func CtrlReqMsg(bmRequestType, bRequest uint8, wValue, wIndex, wLength uint16, data []byte) PipeMsg {
	return PipeMsg{
		Direction:     HostToDevice,
		Type:          MsgControl,
		BMRequestType: bmRequestType,
		BRequest:      bRequest,
		WValue:        wValue,
		WIndex:        wIndex,
		WLength:       wLength,
		Data:          data,
	}
}

// CtrlRespMsg creates a control response PipeMsg
func CtrlRespMsg(data []byte, stall bool) PipeMsg {
	return PipeMsg{
		Direction: DeviceToHost,
		Type:      MsgControl,
		Data:      data,
		Stall:     stall,
	}
}

// EventMsg creates an event PipeMsg
func EventMsg(dir Direction, data []byte) PipeMsg {
	return PipeMsg{
		Direction: dir,
		Type:      MsgEvent,
		Data:      data,
	}
}
