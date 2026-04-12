package serial

import (
	"errors"
)

// 帧头 magic bytes
const (
	Magic1     byte = 0x55
	Magic2     byte = 0xAA
	HeaderLen       = 4 // magic1 + magic2 + len_lo + len_hi
	MaxPayload      = 4095
)

// Frame 表示一帧解码后的数据（CMD + 原始 payload）
type Frame struct {
	Cmd     byte
	Payload []byte
}

// Encoder 帧编码器
type Encoder struct{}

// Encode 将 cmd 和 payload 编码为完整帧字节
// 帧格式: [0x55][0xAA][len_lo][len_hi][cmd][payload...]
func (e *Encoder) Encode(cmd byte, payload []byte) []byte {
	dataLen := uint16(1 + len(payload)) // cmd + payload
	frame := make([]byte, HeaderLen+int(dataLen))
	frame[0] = Magic1
	frame[1] = Magic2
	frame[2] = byte(dataLen)
	frame[3] = byte(dataLen >> 8)
	frame[4] = cmd
	copy(frame[5:], payload)
	return frame
}

// Decoder 流式帧解码器（状态机）
type Decoder struct {
	state   rxState
	dataLen uint16 // cmd+payload 总长度
	pos     int    // 已读取数据字节数
	buf     [MaxPayload]byte
	rawBuf  []byte // 非帧原始字节暂存
}

type rxState int

const (
	stateMagic1 rxState = iota
	stateMagic2
	stateLen1
	stateLen2
	stateData
)

var (
	ErrBadFrame = errors.New("invalid frame")
)

// Feed 向解码器输入原始字节，返回解析出的帧和非帧原始数据
func (d *Decoder) Feed(data []byte) ([]Frame, []byte, error) {
	var frames []Frame

	for _, b := range data {
		switch d.state {
		case stateMagic1:
			if b == Magic1 {
				d.state = stateMagic2
			} else {
				// 非帧数据（bootloader 日志等）
				d.rawBuf = append(d.rawBuf, b)
			}

		case stateMagic2:
			if b == Magic2 {
				d.state = stateLen1
			} else {
				// 假帧头，0x55 是日志的一部分
				d.rawBuf = append(d.rawBuf, Magic1, b)
				d.state = stateMagic1
			}

		case stateLen1:
			d.dataLen = uint16(b)
			d.state = stateLen2

		case stateLen2:
			d.dataLen |= uint16(b) << 8
			if d.dataLen < 1 || d.dataLen > MaxPayload {
				// 无效长度，重置
				d.state = stateMagic1
			} else {
				d.pos = 0
				d.state = stateData
			}

		case stateData:
			d.buf[d.pos] = b
			d.pos++
			if d.pos >= int(d.dataLen) {
				// 完整帧
				frame := Frame{
					Cmd:     d.buf[0],
					Payload: make([]byte, d.dataLen-1),
				}
				copy(frame.Payload, d.buf[1:d.dataLen])
				frames = append(frames, frame)
				d.state = stateMagic1
			}
		}
	}

	// 返回积累的非帧原始数据
	var raw []byte
	if len(d.rawBuf) > 0 {
		raw = make([]byte, len(d.rawBuf))
		copy(raw, d.rawBuf)
		d.rawBuf = d.rawBuf[:0]
	}

	return frames, raw, nil
}
