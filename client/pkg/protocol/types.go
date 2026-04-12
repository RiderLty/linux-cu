package protocol

import (
	"encoding/binary"
	"errors"
)

var (
	ErrPayloadTooShort = errors.New("payload too short")
)

// BindDeviceReq BIND_DEVICE 载荷: VID(2B LE) + PID(2B LE)
type BindDeviceReq struct {
	VID uint16
	PID uint16
}

func (r *BindDeviceReq) Marshal() []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint16(buf[0:2], r.VID)
	binary.LittleEndian.PutUint16(buf[2:4], r.PID)
	return buf
}

func UnmarshalBindDeviceReq(p []byte) (*BindDeviceReq, error) {
	if len(p) < 4 {
		return nil, ErrPayloadTooShort
	}
	return &BindDeviceReq{
		VID: binary.LittleEndian.Uint16(p[0:2]),
		PID: binary.LittleEndian.Uint16(p[2:4]),
	}, nil
}

// DescBundle DESC_BUNDLE 载荷: DESC_ID(1B) + CHUNK_IDX(1B) + CHUNK_TOTAL(1B) + DATA
type DescBundle struct {
	DescID     byte
	ChunkIdx   byte
	ChunkTotal byte
	Data       []byte
}

func (d *DescBundle) Marshal() []byte {
	buf := make([]byte, 3+len(d.Data))
	buf[0] = d.DescID
	buf[1] = d.ChunkIdx
	buf[2] = d.ChunkTotal
	copy(buf[3:], d.Data)
	return buf
}

func UnmarshalDescBundle(p []byte) (*DescBundle, error) {
	if len(p) < 3 {
		return nil, ErrPayloadTooShort
	}
	data := make([]byte, len(p)-3)
	copy(data, p[3:])
	return &DescBundle{
		DescID:     p[0],
		ChunkIdx:   p[1],
		ChunkTotal: p[2],
		Data:       data,
	}, nil
}

// HidReport HID_REPORT 载荷: INTERFACE(1B) + DATA_LEN(1B) + REPORT_DATA
type HidReport struct {
	Interface byte
	Data      []byte
}

func (h *HidReport) Marshal() []byte {
	buf := make([]byte, 2+len(h.Data))
	buf[0] = h.Interface
	buf[1] = byte(len(h.Data))
	copy(buf[2:], h.Data)
	return buf
}

func UnmarshalHidReport(p []byte) (*HidReport, error) {
	if len(p) < 2 {
		return nil, ErrPayloadTooShort
	}
	dataLen := int(p[1])
	if len(p) < 2+dataLen {
		return nil, ErrPayloadTooShort
	}
	data := make([]byte, dataLen)
	copy(data, p[2:2+dataLen])
	return &HidReport{
		Interface: p[0],
		Data:      data,
	}, nil
}

// CtrlReq CTRL_REQ 载荷 (P4 -> Linux)
type CtrlReq struct {
	BMRequestType byte
	BRequest      byte
	WValue        uint16
	WIndex        uint16
	WLength       uint16
	DataPhase     []byte // OUT 传输的数据，IN 传输时为空
}

func UnmarshalCtrlReq(p []byte) (*CtrlReq, error) {
	if len(p) < 8 {
		return nil, ErrPayloadTooShort
	}
	r := &CtrlReq{
		BMRequestType: p[0],
		BRequest:      p[1],
		WValue:        binary.LittleEndian.Uint16(p[2:4]),
		WIndex:        binary.LittleEndian.Uint16(p[4:6]),
		WLength:       binary.LittleEndian.Uint16(p[6:8]),
	}
	if len(p) > 8 {
		r.DataPhase = make([]byte, len(p)-8)
		copy(r.DataPhase, p[8:])
	}
	return r, nil
}

// CtrlData CTRL_DATA 载荷 (Linux -> P4): DATA_LEN(2B LE) + RESPONSE_DATA
type CtrlData struct {
	Data []byte
}

func (c *CtrlData) Marshal() []byte {
	buf := make([]byte, 2+len(c.Data))
	binary.LittleEndian.PutUint16(buf[0:2], uint16(len(c.Data)))
	copy(buf[2:], c.Data)
	return buf
}

// ErrorPayload CMD_ERROR 载荷: ORIGIN_CMD(1B) + ERROR_CODE(1B) + ERROR_MSG(0-128B)
type ErrorPayload struct {
	OriginCmd byte
	ErrorCode byte
	ErrorMsg  string
}

func (e *ErrorPayload) Marshal() []byte {
	msg := []byte(e.ErrorMsg)
	buf := make([]byte, 2+len(msg))
	buf[0] = e.OriginCmd
	buf[1] = e.ErrorCode
	copy(buf[2:], msg)
	return buf
}

func UnmarshalErrorPayload(p []byte) (*ErrorPayload, error) {
	if len(p) < 2 {
		return nil, ErrPayloadTooShort
	}
	ep := &ErrorPayload{
		OriginCmd: p[0],
		ErrorCode: p[1],
	}
	if len(p) > 2 {
		ep.ErrorMsg = string(p[2:])
	}
	return ep, nil
}
