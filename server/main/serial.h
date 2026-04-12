/*
 * serial.h - 通信层
 * 基于 VFS stdin/stdout，帧格式: [0x55][0xAA][len_lo][len_hi][cmd][payload...]
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

/* 缓冲区大小 */
#define SERIAL_TX_BUF_SIZE  2048
#define SERIAL_RX_BUF_SIZE  512

/* 波特率 */
#define SERIAL_BAUD_RATE    4000000

/* 串口配置 */
typedef struct {
    uint32_t tx_buffer_size;
    uint32_t rx_buffer_size;
} serial_config_t;

/* ---- 初始化 ---- */

void serial_init(const serial_config_t *cfg);

/* ---- 接收 ---- */

void serial_rx_feed(const uint8_t *data, uint16_t len);

bool serial_recv_frame(frame_t *out, TickType_t timeout_ticks);

bool serial_recv_frame_nowait(frame_t *out);

/* ---- 发送 ---- */

void serial_send_frame(uint8_t cmd, const uint8_t *payload, uint16_t payload_len);

void serial_send_cmd(uint8_t cmd);

/* ---- 轮询 ---- */

void serial_poll(void);

bool serial_is_connected(void);

#endif /* SERIAL_H */
