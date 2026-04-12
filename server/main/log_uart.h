/*
 * log_uart.h - 将 ESP-IDF 日志通过串口帧转发给 Linux
 */

#ifndef LOG_UART_H
#define LOG_UART_H

/*
 * 初始化 UART 日志转发
 * 注册自定义 vprintf 回调，将日志封装为 CMD_LOG_MSG 帧
 * 通过 serial_send_frame 发送
 * 必须在 serial_init 之后调用
 */
void log_uart_init(void);

#endif /* LOG_UART_H */
