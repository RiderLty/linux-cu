/*
 * serial.h - 通信层 (USB-SERIAL-JTAG 直接读写)
 */

#ifndef SERIAL_H
#define SERIAL_H

#include "frame.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include <stdbool.h>
#include <stdint.h>

/* 接收帧队列深度 */
#define SERIAL_RX_QUEUE_LEN  32

/* ---- 初始化 ---- */

void serial_init(void);

/* ---- 接收 ---- */

bool serial_recv_frame(frame_t *out, TickType_t timeout_ticks);

bool serial_recv_frame_nowait(frame_t *out);

/* ---- 发送 ---- */

void serial_send_frame(uint8_t cmd, const uint8_t *payload, uint16_t payload_len);

void serial_send_cmd(uint8_t cmd);

/* ---- 轮询 (从 USB-SERIAL-JTAG 读取并解码) ---- */

void serial_poll(void);

#endif /* SERIAL_H */
