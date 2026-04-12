package protocol

// 命令字
const (
	CmdPing       byte = 0x01
	CmdPong       byte = 0x02
	CmdGetInfo    byte = 0x10
	CmdInfoResp   byte = 0x11
	CmdBindDevice byte = 0x20
	CmdBindAck    byte = 0x21
	CmdDescBundle byte = 0x30
	CmdDescAck    byte = 0x31
	CmdStartEmul  byte = 0x40
	CmdEmulAck    byte = 0x41
	CmdStopEmul   byte = 0x50
	CmdStopAck    byte = 0x51
	CmdHidReport  byte = 0x60
	CmdCtrlReq    byte = 0x70
	CmdCtrlData   byte = 0x71
	CmdCtrlStall  byte = 0x72
	CmdError      byte = 0xFE
	CmdReset      byte = 0xFF
)

// 握手 magic number (P4 返回的 PONG payload)
const HandshakeMagic = "P4OK"

// DESC_ID 分配
const (
	DescIDDevice     byte = 0x01
	DescIDConfig     byte = 0x02
	DescIDReportBase byte = 0x10 // 0x10 + 接口索引
	DescIDStringBase byte = 0x20 // 0x20 + 字符串描述符索引
)

// 错误码
const (
	ErrCRC     byte = 0x01
	ErrFrame   byte = 0x02
	ErrCmd     byte = 0x03
	ErrState   byte = 0x04
	ErrDesc    byte = 0x05
	ErrUSB     byte = 0x06
	ErrTimeout byte = 0x07
	ErrBusy    byte = 0x08
	ErrUnknown byte = 0xFF
)

// 超时/重试参数
const (
	PingTimeoutMs      = 500
	PingRetries        = 3
	DescAckTimeoutMs   = 2000
	DescRetries        = 3
	CtrlTimeoutMs      = 1000
	StartEmulTimeoutMs = 5000
	StartEmulRetries   = 1
	HeartbeatIntervalS = 5
	HeartbeatMissMax   = 3
)

// MaxDescChunk 单帧最大描述符数据量 = 4092 - 3 (DESC_ID + CHUNK_IDX + CHUNK_TOTAL)
const MaxDescChunk = 4089
