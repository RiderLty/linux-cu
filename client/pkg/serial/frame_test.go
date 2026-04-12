package serial

import (
	"bytes"
	"testing"
)

func TestEncodePingFrame(t *testing.T) {
	enc := &Encoder{}
	frame := enc.Encode(0x01, nil) // PING, no payload

	// 验证帧头
	if frame[0] != Magic1 || frame[1] != Magic2 {
		t.Errorf("header = 0x%02X 0x%02X, want 0x55 0xAA", frame[0], frame[1])
	}

	// LEN = 1 (只有 CMD)
	// 完整帧: 0x55 0xAA len_lo len_hi cmd = 5 bytes
	if len(frame) != 5 {
		t.Errorf("frame length = %d, want 5", len(frame))
	}

	// LEN 字段
	if frame[2] != 0x01 || frame[3] != 0x00 {
		t.Errorf("len field = 0x%02X 0x%02X, want 0x01 0x00", frame[2], frame[3])
	}

	// CMD
	if frame[4] != 0x01 {
		t.Errorf("cmd = 0x%02X, want 0x01", frame[4])
	}
}

func TestRoundtrip(t *testing.T) {
	enc := &Encoder{}
	dec := &Decoder{}

	payload := []byte{0x01, 0x02, 0x03, 0x04}
	frame := enc.Encode(0x30, payload)

	frames, _, err := dec.Feed(frame)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}

	f := frames[0]
	if f.Cmd != 0x30 {
		t.Errorf("cmd = 0x%02X, want 0x30", f.Cmd)
	}
	if !bytes.Equal(f.Payload, payload) {
		t.Errorf("payload = %v, want %v", f.Payload, payload)
	}
}

func TestAllByteValues(t *testing.T) {
	enc := &Encoder{}
	dec := &Decoder{}

	// 测试所有字节值（包括 0x55 和 0x7E 等）都能正确传输
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}

	frame := enc.Encode(0x10, payload)

	frames, _, err := dec.Feed(frame)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if !bytes.Equal(frames[0].Payload, payload) {
		t.Error("payload roundtrip mismatch for all-byte-values test")
	}
}

func TestDecodeMultipleFrames(t *testing.T) {
	enc := &Encoder{}
	dec := &Decoder{}

	frame1 := enc.Encode(0x01, nil)
	frame2 := enc.Encode(0x10, []byte{0xAA, 0xBB})
	frame3 := enc.Encode(0x20, []byte{0x01})

	// 拼接多个帧
	combined := append(frame1, frame2...)
	combined = append(combined, frame3...)

	frames, _, err := dec.Feed(combined)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}
	if frames[0].Cmd != 0x01 || frames[1].Cmd != 0x10 || frames[2].Cmd != 0x20 {
		t.Errorf("wrong cmds: 0x%02X 0x%02X 0x%02X", frames[0].Cmd, frames[1].Cmd, frames[2].Cmd)
	}
}

func TestDecodeByteByByte(t *testing.T) {
	enc := &Encoder{}
	dec := &Decoder{}

	payload := []byte{0x11, 0x22, 0x33}
	frame := enc.Encode(0x40, payload)

	// 逐字节喂入
	var allFrames []Frame
	for _, b := range frame {
		frames, _, _ := dec.Feed([]byte{b})
		allFrames = append(allFrames, frames...)
	}
	if len(allFrames) != 1 {
		t.Fatalf("got %d frames, want 1", len(allFrames))
	}
	if allFrames[0].Cmd != 0x40 {
		t.Errorf("cmd = 0x%02X, want 0x40", allFrames[0].Cmd)
	}
	if !bytes.Equal(allFrames[0].Payload, payload) {
		t.Errorf("payload = %v, want %v", allFrames[0].Payload, payload)
	}
}

func TestDecodeIgnoreNoise(t *testing.T) {
	enc := &Encoder{}
	dec := &Decoder{}

	frame := enc.Encode(0x01, nil)

	// 在帧前后加入噪声
	noisy := append([]byte{0x00, 0xFF, 0x55, 0x12}, frame...)
	noisy = append(noisy, 0x34, 0x56)

	frames, raw, err := dec.Feed(noisy)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	// 应该正确解析出一个帧
	found := false
	for _, f := range frames {
		if f.Cmd == 0x01 {
			found = true
		}
	}
	if !found {
		t.Error("did not find expected PING frame among noise")
	}
	// 噪声中的 0x55 后面不是 0xAA，应作为原始数据
	if len(raw) == 0 {
		t.Error("expected some raw data from noise")
	}
}

func TestLargePayload(t *testing.T) {
	enc := &Encoder{}
	dec := &Decoder{}

	// 最大 payload
	payload := make([]byte, MaxPayload-1) // MaxPayload = cmd + payload max
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	frame := enc.Encode(0x30, payload)
	frames, _, err := dec.Feed(frame)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if !bytes.Equal(frames[0].Payload, payload) {
		t.Error("large payload roundtrip mismatch")
	}
}
