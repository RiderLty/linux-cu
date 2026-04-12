/*
 * commands.h - 协议命令字、描述符 ID、错误码定义
 * 与 Go 端 pkg/protocol/commands.go 保持一致
 */

#ifndef COMMANDS_H
#define COMMANDS_H

#include <stdint.h>

/* ---- 命令字 ---- */
#define CMD_PING        0x01
#define CMD_PONG        0x02
#define CMD_GET_INFO    0x10
#define CMD_INFO_RESP   0x11
#define CMD_BIND_DEVICE 0x20
#define CMD_BIND_ACK    0x21
#define CMD_DESC_BUNDLE 0x30
#define CMD_DESC_ACK    0x31
#define CMD_START_EMUL  0x40
#define CMD_EMUL_ACK    0x41
#define CMD_STOP_EMUL   0x50
#define CMD_STOP_ACK    0x51
#define CMD_HID_REPORT  0x60
#define CMD_CTRL_REQ    0x70
#define CMD_CTRL_DATA   0x71
#define CMD_CTRL_STALL  0x72
#define CMD_LOG_MSG     0x80
#define CMD_ERROR       0xFE
#define CMD_RESET       0xFF

/* ---- DESC_ID 分配 ---- */
#define DESC_ID_DEVICE      0x01
#define DESC_ID_CONFIG      0x02
#define DESC_ID_REPORT_BASE 0x10  /* 0x10 + 接口索引 */
#define DESC_ID_STRING_BASE 0x20  /* 0x20 + 字符串描述符索引 */

/* ---- 错误码 ---- */
#define ERR_CRC     0x01
#define ERR_FRAME   0x02
#define ERR_CMD     0x03
#define ERR_STATE   0x04
#define ERR_DESC    0x05
#define ERR_USB     0x06
#define ERR_TIMEOUT 0x07
#define ERR_BUSY    0x08
#define ERR_UNKNOWN 0xFF

/* ---- 超时参数 (ms) ---- */
#define CTRL_TIMEOUT_MS     1000
#define PING_INTERVAL_MS    5000

/* ---- 描述符参数 ---- */
#define MAX_DESC_CHUNK_DATA 4089

/* ---- 最大 HID 接口数 ---- */
#define MAX_HID_INTERFACES  4

/* ---- 最大描述符大小 ---- */
#define MAX_DEVICE_DESC_LEN   18
#define MAX_CONFIG_DESC_LEN   2048
#define MAX_REPORT_DESC_LEN   512
#define MAX_STRING_DESC_LEN   256

#endif /* COMMANDS_H */
