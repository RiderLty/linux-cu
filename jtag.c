#include <stdio.h>
#include <string.h>
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "driver/usb_serial_jtag.h"

void app_main(void)
{
    // 1. 初始化USB-Serial-JTAG驱动
    usb_serial_jtag_driver_config_t usb_cfg = USB_SERIAL_JTAG_DRIVER_CONFIG_DEFAULT();
    usb_serial_jtag_driver_install(&usb_cfg);
    printf("USB Serial JTAG driver installed.\n");

    uint8_t data[64];
    while (1) {
        // 2. 读取上位机发来的数据
        int len = usb_serial_jtag_read_bytes(data, sizeof(data), pdMS_TO_TICKS(10));
        if (len > 0) {
            // 3. 将接收到的数据原样发回上位机（Echo示例）
            usb_serial_jtag_write_bytes(data, len, portMAX_DELAY);
        }

        // 4. 定期发送自定义心跳包
        const char* heartbeat = "Ping from ESP32-P4\n";
        usb_serial_jtag_write_bytes((const uint8_t*)heartbeat, strlen(heartbeat), portMAX_DELAY);

        vTaskDelay(pdMS_TO_TICKS(1000));
    }
}