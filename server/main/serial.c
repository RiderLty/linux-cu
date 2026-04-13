/*
 * serial.c - 通信层实现 (USB-SERIAL-JTAG 直接读写)
 * 参考 jtag.c: 使用 usb_serial_jtag_read/write_bytes
 */

#include "serial.h"
#include "driver/usb_serial_jtag.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include <string.h>

/* ---- 内部状态 ---- */
static frame_decoder_t s_decoder;
static QueueHandle_t   s_rx_queue = NULL;
static SemaphoreHandle_t s_tx_mutex = NULL;

/* 接收缓冲 */
static uint8_t s_rx_buf[512];

/* 发送帧缓冲 (static，避免栈溢出) */
static uint8_t s_tx_buf[FRAME_HEADER + FRAME_MAX_DATA];

/* ---- 公开接口 ---- */

void serial_init(void) {
    frame_decoder_init(&s_decoder);

    if (s_rx_queue == NULL) {
        s_rx_queue = xQueueCreate(SERIAL_RX_QUEUE_LEN, sizeof(frame_t));
        assert(s_rx_queue != NULL);
    }

    if (s_tx_mutex == NULL) {
        s_tx_mutex = xSemaphoreCreateMutex();
        assert(s_tx_mutex != NULL);
    }

    /* 安装 USB-SERIAL-JTAG 驱动 */
    usb_serial_jtag_driver_config_t cfg = USB_SERIAL_JTAG_DRIVER_CONFIG_DEFAULT();
    usb_serial_jtag_driver_install(&cfg);
}

static void serial_rx_feed(const uint8_t *data, uint16_t len) {
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

void serial_poll(void) {
    int n = usb_serial_jtag_read_bytes(s_rx_buf, sizeof(s_rx_buf), pdMS_TO_TICKS(10));
    if (n > 0) {
        serial_rx_feed(s_rx_buf, (uint16_t)n);
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
    usb_serial_jtag_write_bytes(s_tx_buf, frame_len, portMAX_DELAY);

    if (s_tx_mutex) xSemaphoreGive(s_tx_mutex);
}

void serial_send_cmd(uint8_t cmd) {
    serial_send_frame(cmd, NULL, 0);
}
