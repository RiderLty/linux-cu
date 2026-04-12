package protocol

import (
	"bytes"
	"testing"
)

func TestBindDeviceRoundtrip(t *testing.T) {
	req := &BindDeviceReq{VID: 0x046D, PID: 0xC08B}
	p := req.Marshal()

	got, err := UnmarshalBindDeviceReq(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.VID != 0x046D || got.PID != 0xC08B {
		t.Errorf("got VID=%04X PID=%04X, want 046D:C08B", got.VID, got.PID)
	}
}

func TestBindDeviceTooShort(t *testing.T) {
	_, err := UnmarshalBindDeviceReq([]byte{0x01, 0x02})
	if err != ErrPayloadTooShort {
		t.Errorf("expected ErrPayloadTooShort, got %v", err)
	}
}

func TestDescBundleRoundtrip(t *testing.T) {
	d := &DescBundle{
		DescID:     DescIDDevice,
		ChunkIdx:   0,
		ChunkTotal: 3,
		Data:       []byte{0x12, 0x34, 0x56, 0x78},
	}
	p := d.Marshal()

	got, err := UnmarshalDescBundle(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.DescID != DescIDDevice || got.ChunkIdx != 0 || got.ChunkTotal != 3 {
		t.Errorf("header mismatch")
	}
	if !bytes.Equal(got.Data, d.Data) {
		t.Errorf("data mismatch: %v vs %v", got.Data, d.Data)
	}
}

func TestHidReportRoundtrip(t *testing.T) {
	h := &HidReport{
		Interface: 2,
		Data:      []byte{0x01, 0x02, 0x03},
	}
	p := h.Marshal()

	got, err := UnmarshalHidReport(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Interface != 2 {
		t.Errorf("interface = %d, want 2", got.Interface)
	}
	if !bytes.Equal(got.Data, h.Data) {
		t.Errorf("data mismatch")
	}
}

func TestHidReportEmpty(t *testing.T) {
	h := &HidReport{Interface: 0, Data: nil}
	p := h.Marshal() // [0x00, 0x00]

	got, err := UnmarshalHidReport(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != 0 {
		t.Errorf("expected empty data, got %v", got.Data)
	}
}

func TestCtrlReqUnmarshal(t *testing.T) {
	// bmRequest=0x80, bRequest=0x06, wValue=0x0100, wIndex=0x0000, wLength=18
	p := []byte{0x80, 0x06, 0x00, 0x01, 0x00, 0x00, 0x12, 0x00}
	got, err := UnmarshalCtrlReq(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.BMRequestType != 0x80 || got.BRequest != 0x06 {
		t.Errorf("request fields wrong")
	}
	if got.WValue != 0x0100 {
		t.Errorf("wValue = 0x%04X, want 0x0100", got.WValue)
	}
	if got.WLength != 18 {
		t.Errorf("wLength = %d, want 18", got.WLength)
	}
	if got.DataPhase != nil {
		t.Errorf("expected nil DataPhase for IN transfer")
	}
}

func TestCtrlReqWithDataPhase(t *testing.T) {
	// OUT transfer with 4 bytes data
	p := []byte{0x00, 0x09, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}
	got, err := UnmarshalCtrlReq(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.DataPhase, []byte{0xAA, 0xBB, 0xCC, 0xDD}) {
		t.Errorf("DataPhase mismatch: %v", got.DataPhase)
	}
}

func TestCtrlDataRoundtrip(t *testing.T) {
	c := &CtrlData{Data: []byte{0x01, 0x02, 0x03, 0x04}}
	p := c.Marshal()

	// Marshal 格式: DATA_LEN(2B LE) + DATA
	if len(p) != 2+4 {
		t.Fatalf("length = %d, want 6", len(p))
	}
	// 验证长度字段
	dataLen := uint16(p[0]) | uint16(p[1])<<8
	if dataLen != 4 {
		t.Errorf("dataLen = %d, want 4", dataLen)
	}
	if !bytes.Equal(p[2:], c.Data) {
		t.Error("data mismatch")
	}
}

func TestErrorPayloadRoundtrip(t *testing.T) {
	e := &ErrorPayload{
		OriginCmd: CmdBindDevice,
		ErrorCode: ErrTimeout,
		ErrorMsg:  "connection lost",
	}
	p := e.Marshal()

	got, err := UnmarshalErrorPayload(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.OriginCmd != CmdBindDevice || got.ErrorCode != ErrTimeout {
		t.Errorf("header mismatch")
	}
	if got.ErrorMsg != "connection lost" {
		t.Errorf("msg = %q, want %q", got.ErrorMsg, "connection lost")
	}
}

func TestErrorPayloadNoMsg(t *testing.T) {
	e := &ErrorPayload{OriginCmd: 0x01, ErrorCode: 0xFF}
	p := e.Marshal()

	got, err := UnmarshalErrorPayload(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.ErrorMsg != "" {
		t.Errorf("expected empty msg, got %q", got.ErrorMsg)
	}
}
