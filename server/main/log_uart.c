/*
 * log_uart.c - 将 ESP-IDF 日志通过串口帧转发给 Linux
 *
 * 使用 esp_log_set_vprintf 拦截 ESP_LOG* 输出，
 * 格式化后封装为 CMD_LOG_MSG 帧，通过 serial_send_frame 发送。
 *
 * 注意: vprintf 回调在 ESP-IDF 日志互斥锁内被调用，
 * 不要在回调内调用可能产生日志的函数（避免递归）。
 * serial_send_frame 使用独立的 tx_mutex，不会死锁。
 */

#include "log_uart.h"
#include "commands.h"
#include "serial.h"
#include "esp_log.h"

#include <stdio.h>
#include <string.h>
#include <stdarg.h>

#define LOG_MSG_MAX_LEN 512

static int log_uart_vprintf(const char *fmt, va_list args) {
    /* 格式化日志 */
    char buf[LOG_MSG_MAX_LEN];
    int len = vsnprintf(buf, sizeof(buf), fmt, args);
    if (len <= 0) return len;

    /* 截断超长消息 */
    if (len >= LOG_MSG_MAX_LEN) len = LOG_MSG_MAX_LEN - 1;

    /* 通过串口发送为 CMD_LOG_MSG 帧 */
    serial_send_frame(CMD_LOG_MSG, (const uint8_t *)buf, (uint16_t)len);

    return len;
}

void log_uart_init(void) {
    /* 替换 vprintf，所有 ESP_LOG* 输出将通过帧协议发送 */
    esp_log_set_vprintf(log_uart_vprintf);
}
