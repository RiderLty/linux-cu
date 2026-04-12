/*
 * serial.c - 通信层实现
 *
 * 硬件初始化在 app_main 中完成（照抄 example.c:27 + 4M 波特率）。
 * serial_init 负责 termios 配置和帧解码器初始化。
 * 数据通过 VFS stdin/stdout 读写。
 */

#include "serial.h"
#include "esp_vfs_dev.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include <stdio.h>
#include <unistd.h>
#include <string.h>
#include <termios.h>

/* ---- 内部状态 ---- */
static frame_decoder_t s_decoder;
static QueueHandle_t   s_rx_queue = NULL;
static SemaphoreHandle_t s_tx_mutex = NULL;

/* 接收缓冲 */
static uint8_t s_rx_buf[SERIAL_RX_BUF_SIZE];

/* 发送帧缓冲（static，避免栈溢出，已有 s_tx_mutex 保护） */
static uint8_t s_tx_buf[FRAME_HEADER + FRAME_MAX_DATA];

/* ---- 轮询 ---- */

void serial_poll(void) {
    int n = read(fileno(stdin), s_rx_buf, sizeof(s_rx_buf));
    if (n > 0) {
        serial_rx_feed(s_rx_buf, (uint16_t)n);
    }
}

/* ---- 公开接口 ---- */

void serial_init(const serial_config_t *cfg) {
    (void)cfg;

    frame_decoder_init(&s_decoder);

    if (s_rx_queue == NULL) {
        s_rx_queue = xQueueCreate(SERIAL_RX_QUEUE_LEN, sizeof(frame_t));
        assert(s_rx_queue != NULL);
    }

    if (s_tx_mutex == NULL) {
        s_tx_mutex = xSemaphoreCreateMutex();
        assert(s_tx_mutex != NULL);
    }

    /* termios 配置: 禁用行缓冲、回显、信号 */
    int fd = fileno(stdin);
    struct termios tios;
    tcgetattr(fd, &tios);
    tios.c_iflag &= ~(INLCR | ICRNL | IGNCR | IXON);
    tios.c_lflag &= ~(ICANON | ECHO | ECHOE | ISIG);
    tcsetattr(fd, TCSANOW, &tios);
}

void serial_rx_feed(const uint8_t *data, uint16_t len) {
    frame_t frame;
    uint16_t offset = 0;

    while (offset < len) {
        int result = frame_decoder_feed(&s_decoder, data + offset,
                                        len - offset, &frame);
        if (result == 1) {
            if (xQueueSend(s_rx_queue, &frame, 0) != pdTRUE) {
                /* 队列满，丢弃 */
            }
            offset++;
        } else {
            offset++;
        }
    }
}

bool serial_recv_frame(frame_t *out, TickType_t timeout_ticks) {
    if (s_rx_queue == NULL) return false;
    return xQueueReceive(s_rx_queue, out, timeout_ticks) == pdTRUE;
}

bool serial_recv_frame_nowait(frame_t *out) {
    return serial_recv_frame(out, 0);
}

void serial_send_frame(uint8_t cmd, const uint8_t *payload, uint16_t payload_len) {
    if (s_tx_mutex) xSemaphoreTake(s_tx_mutex, portMAX_DELAY);

    uint16_t frame_len = frame_encode(s_tx_buf, cmd, payload, payload_len);

    write(fileno(stdout), s_tx_buf, frame_len);

    if (s_tx_mutex) xSemaphoreGive(s_tx_mutex);
}

void serial_send_cmd(uint8_t cmd) {
    serial_send_frame(cmd, NULL, 0);
}

bool serial_is_connected(void) {
    return true;
}
