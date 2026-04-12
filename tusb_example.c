/*
 * SPDX-FileCopyrightText: 2022-2025 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Unlicense OR CC0-1.0
 */

#include "driver/gpio.h"
#include "driver/uart.h"
#include "driver/uart_vfs.h"
#include "driver/usb_serial_jtag.h"
#include "ds_struc.h"
#include "esp_event.h"
#include "esp_log.h"
#include "esp_system.h"
#include "esp_timer.h"
#include "esp_vfs_dev.h"
#include "esp_wifi.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/task.h"
#include "led_helper.h"
#include "lwip/err.h"
#include "lwip/sockets.h"
#include "lwip/sys.h"
#include "nvs_flash.h"
#include "nvs_helper.h"
#include "tusb_helper.h"
#include "udp_helper.h"
#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <termios.h>

static const char *TAG = "HIDcon-Device";

/* 协议命令码 */
#define CMD_WIFI_CONFIG 0xFF
#define CMD_DS5 0
#define CMD_KEYBOARD 1
#define CMD_MOUSE 2
#define CMD_TOUCH 3

#define DEBUG 1
// #define CTRL_BAUD_RATE (20000000)
#if DEBUG
#define CTRL_BAUD_RATE 115200
#else
#define CTRL_BAUD_RATE 20000000
#endif

static uint8_t wifi_status = 0; // 0:未连接(连接中),1:已连接,2:连接失败

#if CONFIG_IDF_TARGET_ESP32S3
#define APP_BUTTON (GPIO_NUM_0)
#elif CONFIG_IDF_TARGET_ESP32P4
#define APP_BUTTON (GPIO_NUM_35)
#else
#define APP_BUTTON (GPIO_NUM_0)
#endif
#define APP_BUTTON_DOWN (0)
#define APP_BUTTON_UP (1)

volatile int udp_sock = -1;

#define HID_QUEUE_DEPTH 16
#define HID_QUEUE_ITEM_SIZE 64
static QueueHandle_t hid_queue = NULL;
static uint32_t uart_drop_count = 0;

#define ENQUEUE_PAYLOAD(payload, len)                                          \
  do {                                                                         \
    uint8_t _item[HID_QUEUE_ITEM_SIZE] = {0};                                  \
    memcpy(_item, (payload), (len));                                           \
    if (xQueueSend(hid_queue, _item, 0) != pdTRUE) {                           \
      uint8_t _dummy[HID_QUEUE_ITEM_SIZE];                                     \
      xQueueReceive(hid_queue, _dummy, 0);                                     \
      xQueueSend(hid_queue, _item, 0);                                         \
      uart_drop_count++;                                                       \
      if (uart_drop_count % 100 == 0)                                          \
        ESP_LOGW(TAG, "Queue full, dropped %lu packets", uart_drop_count);     \
    }                                                                          \
  } while (0)

#define WIFI_CONNECTED_BIT BIT0
#define WIFI_FAIL_BIT BIT1

#define EXTERNAL_CTRL_ENABLE 1

#if CONFIG_IDF_TARGET_ESP32S3
#define EXTERNAL_CTRL_UART_RX (GPIO_NUM_2)
#define EXTERNAL_CTRL_UART_TX (GPIO_NUM_1)
#elif CONFIG_IDF_TARGET_ESP32P4
#define EXTERNAL_CTRL_UART_RX (GPIO_NUM_15)
#define EXTERNAL_CTRL_UART_TX (GPIO_NUM_14)
#else
#define APP_BUTTON (GPIO_NUM_0)
#endif
#define EXTERNAL_CTRL_UART_BAUD_RATE (CTRL_BAUD_RATE)

static void usb_event_handler(tusbh_event_t event, void *ctx) {
  (void)ctx;
  switch (event) {
  case TUSBH_EVENT_ATTACHED:
    ESP_LOGI(TAG, "USB connect");
    xTaskCreatePinnedToCore(LED_red_ok, "LED_red_ok", 2048, NULL, 1, NULL, 0);
    break;
  case TUSBH_EVENT_DETACHED:
    ESP_LOGI(TAG, "USB disconnect");
    LED_red_warning();
    break;
  case TUSBH_EVENT_SUSPENDED:
    ESP_LOGI(TAG, "USB suspend");
    LED_red_warning();
    break;
  case TUSBH_EVENT_RESUMED:
    ESP_LOGI(TAG, "USB resume");
    xTaskCreatePinnedToCore(LED_red_ok, "LED_red_ok", 2048, NULL, 1, NULL, 0);
    break;
  }
}

void test_hid(void) {
  ESP_LOGI(TAG, "===== HID 测试开始 =====");

  /*----- DS5 手柄测试: 逐个按下所有15个按键 -----*/
  while (!(tusbh_is_ready() && tusbh_hid_ready(0))) {
    vTaskDelay(pdMS_TO_TICKS(1));
  }
#define SEND_DS5_REPORT(n)                                                     \
  do {                                                                         \
    while (!(tusbh_is_ready() && tusbh_hid_ready(0))) {                        \
    }                                                                          \
    tusbh_send_ds5_report((const uint8_t *)&rpt, sizeof(rpt));                 \
    vTaskDelay(pdMS_TO_TICKS(n));                                              \
  } while (0)
  struct dualsense_input_report rpt;
  memset(&rpt, 0, sizeof(rpt));
  rpt.points_1.contact = 0xFF;
  rpt.points_2.contact = 0xFF;
  rpt.lt = 0;
  rpt.rt = 0;
  rpt.ls_x = 0x7f;
  rpt.ls_y = 0x7d;
  rpt.rs_x = 0x7f;
  rpt.rs_y = 0x7e;
  rpt.buttons.dpad = DS_HAT_NULL;
  SEND_DS5_REPORT(1); // 初始化
  uint32_t btn = 1 << 4;
  for (uint32_t i = 0; i < 8; i++) {
    rpt.buttons.dpad = i;
    SEND_DS5_REPORT(100);
  }
  rpt.buttons.dpad = DS_HAT_NULL;
  SEND_DS5_REPORT(1);
  for (int i = 0; i < 15; i++) {
    rpt.buttons.raw ^= btn;
    SEND_DS5_REPORT(150);
    rpt.buttons.raw ^= btn;
    SEND_DS5_REPORT(50);
    btn <<= 1;
  }

  /* 左右摇杆同步画圆 */
  for (int angle = 0; angle < 360; angle += 5) {
    float rad = angle * 3.14159265f / 180.0f;
    uint8_t cx = (uint8_t)(127.0f + 127.0f * cosf(rad));
    uint8_t cy = (uint8_t)(127.0f + 127.0f * sinf(rad));
    rpt.ls_x = cx;
    rpt.ls_y = cy;
    rpt.rs_x = cy;
    rpt.rs_y = cx;
    SEND_DS5_REPORT(16);
  }
  rpt.ls_x = 0x7f;
  rpt.ls_y = 0x7d;
  rpt.rs_x = 0x7f;
  rpt.rs_y = 0x7e;
  SEND_DS5_REPORT(1);
  /*测试扳机*/
  for (uint8_t i = 0; i < 0xff; i++) {
    rpt.lt = i;
    rpt.rt = i;
    SEND_DS5_REPORT(4);
  }
  rpt.lt = 0;
  rpt.rt = 0;
  SEND_DS5_REPORT(1);

  ESP_LOGI(TAG, "DS5 手柄测试完成");

  /*----- 键盘测试: 输入 "hello" -----*/
  ESP_LOGI(TAG, "测试键盘: 输入 hello");
  uint8_t keys_hello[] = {KeyH, KeyE, KeyL, KeyL, KeyO};
  for (int i = 0; i < sizeof(keys_hello); i++) {
    uint8_t keycode[6] = {keys_hello[i], 0, 0, 0, 0, 0};
    tusbh_send_keyboard_report(0, keycode);
    vTaskDelay(pdMS_TO_TICKS(50));
    tusbh_send_keyboard_report(0, NULL); // 释放
    vTaskDelay(pdMS_TO_TICKS(50));
  }
  ESP_LOGI(TAG, "键盘测试完成");

  /*----- 鼠标测试: 交替移动 (100,100) (-100,-100) 1000次 -----*/
  ESP_LOGI(TAG, "测试鼠标 ");
  int64_t start = esp_timer_get_time();
  for (int i = 0; i < 1000; i++) {
    tusbh_send_mouse_report(0, 100, 0, 0, 0);
    while (!(tusbh_is_ready() && tusbh_hid_ready(2))) {
    }
    tusbh_send_mouse_report(100, 0, 0, 0, 0);
    while (!(tusbh_is_ready() && tusbh_hid_ready(2))) {
    }
    tusbh_send_mouse_report(0, -100, 0, 0, 0);
    while (!(tusbh_is_ready() && tusbh_hid_ready(2))) {
    }
    tusbh_send_mouse_report(-100, 0, 0, 0, 0);
    while (!(tusbh_is_ready() && tusbh_hid_ready(2))) {
    }
  }
  int64_t end = esp_timer_get_time();
  ESP_LOGI(TAG,
           "鼠标测试完成，耗时 %lld 微秒, 共发送 %u 个报告, 发送频率%lld Hz",
           end - start, 4000, 4000000000 / ((end - start)));

  /*----- 触摸屏测试 -----*/
  ESP_LOGI(TAG, "测试触摸屏");
  tusbh_remote_wakeup();
  while (!(tusbh_is_ready() && tusbh_hid_ready(3))) {
    vTaskDelay(pdMS_TO_TICKS(1));
  }
  tusbh_touch_report_t r = {
      .state = 0x01,
      .id = 0x00,
      .x = 0x00,
      .y = 0x00,
      .flag = 0x00,
  };
  uint32_t counter = 0;
  start = esp_timer_get_time();
  for (uint32_t i = 0; i < 0x7ffffffe; i += 0x7ffffffe / 1000) {
    r.id = 0x00;
    r.x = 0x7ffffffe - i;
    r.y = 0x7ffffffe - i;
    while (!(tusbh_is_ready() && tusbh_hid_ready(3))) {
    }
    tusbh_send_touch_report(&r);
    r.id = 0x01;
    r.y = i;
    while (!(tusbh_is_ready() && tusbh_hid_ready(3))) {
    }
    tusbh_send_touch_report(&r);
    counter++;
  }
  end = esp_timer_get_time();
  ESP_LOGI(
      TAG,
      "触摸屏测试完成，共发送 %u 个报告，耗时 %lld 微秒 , 发送频率 %lld Hz",
      counter << 1, end - start, (counter << 1) * 1000000 / ((end - start)));
  r.state = 0x00;
  r.id = 0x00;
  while (!(tusbh_is_ready() && tusbh_hid_ready(3))) {
    vTaskDelay(pdMS_TO_TICKS(1));
  }
  tusbh_send_touch_report(&r);
  r.state = 0x00;
  r.id = 0x01;
  while (!(tusbh_is_ready() && tusbh_hid_ready(3))) {
    vTaskDelay(pdMS_TO_TICKS(1));
  }
  tusbh_send_touch_report(&r);
  vTaskDelay(pdMS_TO_TICKS(1));

  ESP_LOGI(TAG, "===== HID 测试完成 =====");
}

/********* Application ***************/

/*
 * 高优先级HID发送任务
 */
static void hid_sender_task(void *arg) {
  tusbh_set_hid_task_handle(xTaskGetCurrentTaskHandle());
  uint8_t buf[HID_QUEUE_ITEM_SIZE];
  ESP_LOGI(TAG, "HID sender task started");
  while (1) {
    if (tusbh_is_suspended()) {
      tusbh_remote_wakeup();
      vTaskDelay(pdMS_TO_TICKS(100));
      continue;
    }
    if (xQueueReceive(hid_queue, buf, pdMS_TO_TICKS(100)) != pdTRUE)
      continue;
    uint8_t cmd = buf[0];
    if (!tusbh_is_ready())
      continue;
    // 等待 USB 端点就绪（由 tud_hid_report_complete_cb 唤醒）
    while (!tusbh_hid_ready(cmd)) {
      ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(100));
      if (!tusbh_is_ready())
        break;
    }
    if (!tusbh_is_ready())
      continue;
    uint8_t report_id = (cmd == CMD_DS5 || cmd == CMD_KEYBOARD) ? 1 : 0;
    // buf[1..63] 是报告数据，长度由 cmd 决定
    static const uint8_t report_len[] = {
        DS5_INPUT_REPORT_SIZE,          // CMD_DS5=0
        KEYBOARD_INPUT_REPORT_SIZE,     // CMD_KEYBOARD=1
        MOUSE_INPUT_REPORT_SIZE,        // CMD_MOUSE=2
        TOUCH_SCREEN_INPUT_REPORT_SIZE, // CMD_TOUCH=3
    };
    if (cmd <= CMD_TOUCH) {
      if (!tusbh_send_n_report(cmd, report_id, buf + 1, report_len[cmd])) {
        ESP_LOGE(TAG, "Failed to send report, cmd=%u", cmd);
      }
    }
  }
}
/*
 * UART接收任务
 */
typedef enum {
  STATE_WAIT_H1,
  STATE_WAIT_H2,
  STATE_WAIT_LEN,
  STATE_WAIT_PAYLOAD,
  STATE_WAIT_CHECKSUM
} rx_state_t;

#define MAX_PACK_SIZE 1024
static uint8_t rx_buffer[MAX_PACK_SIZE];
static int rx_buffer_len = -1;
static rx_state_t state = STATE_WAIT_H1;
static uint8_t expected_len = 0;
static uint8_t current_idx = 0;
static uint8_t checksum = 0;
static uint8_t payload_buf[MAX_PACK_SIZE];

esp_err_t process_uart_payload(uint8_t *payload, size_t len) {
#if DEBUG
  // printf("处理数据[%d]:[ ", len);
  // for (int i = 0; i < len; i++) {
  //   printf("%02x ", payload[i]);
  // }
  // printf("]\n");
#endif

  uint8_t cmd = payload[0];
  switch (cmd) {
  case CMD_WIFI_CONFIG: { // WiFi配置 : payload[1]=ssid_len,
                          // payload[2]=password_len, payload[3..ssid_len]=ssid,
                          // payload[ssid_len..ssid_len+password_len]=password
    if (len < 3)
      return ESP_FAIL;
    char ssid[33];
    char password[65];
    uint8_t ssid_len = payload[1] > 32 ? 32 : payload[1];
    uint8_t password_len = payload[2] > 64 ? 64 : payload[2];
    memcpy(ssid, payload + 3, ssid_len);
    memcpy(password, payload + 3 + ssid_len, password_len);
    ssid[ssid_len] = 0x00;
    password[password_len] = 0x00;
    ESP_LOGI(TAG, "SSID: %s, Password: %s", ssid, password);
    char *old_ssid = nvs_helper_get_str("ssid");
    char *old_password = nvs_helper_get_str("password");
    bool need_update =
        (old_ssid == NULL || old_password == NULL ||
         strcmp(old_ssid, ssid) != 0 || strcmp(old_password, password) != 0);
    free(old_ssid);
    free(old_password);
    if (need_update) {
      nvs_helper_set_str("ssid", ssid);
      nvs_helper_set_str("password", password);
      ESP_LOGI(TAG, "SSID and Password updated");
      ESP_LOGI(TAG, "REBOOTING...");
      vTaskDelay(pdMS_TO_TICKS(1000));
      esp_restart();
    }
    break;
  }
  case CMD_DS5: { // DS5手柄: payload[0]=cmd, payload[1..]=report data
    if (len != DS5_INPUT_REPORT_SIZE + 1) {
      ESP_LOGW(TAG, "DS5 report len error, expect %d, got %d",
               DS5_INPUT_REPORT_SIZE + 1, len);
      return ESP_FAIL;
    }
    ENQUEUE_PAYLOAD(payload, len);
    break;
  }
  case CMD_KEYBOARD: { // 键盘: payload[0]=cmd, payload[1]=modifier,
                       // payload[2..7]=keycodes
    if (len != KEYBOARD_INPUT_REPORT_SIZE + 1) {
      ESP_LOGW(TAG, "Keyboard report len error, expect %d, got %d",
               KEYBOARD_INPUT_REPORT_SIZE + 1, len);
      return ESP_FAIL;
    }
    ENQUEUE_PAYLOAD(payload, len);
    break;
  }
  case CMD_MOUSE: { // 鼠标: payload[0]=cmd, payload[1]=buttons, [2]=x, [3]=y,
                    // [4]=wheel, [5]=pan
    if (len != MOUSE_INPUT_REPORT_SIZE + 1) {
      ESP_LOGW(TAG, "Mouse report len error, expect %d, got %d",
               MOUSE_INPUT_REPORT_SIZE + 1, len);
      return ESP_FAIL;
    }
    ENQUEUE_PAYLOAD(payload, len);
    break;
  }
  case CMD_TOUCH: { // 触控: payload[0]=cmd, payload[1..]=report data
    if (len != TOUCH_SCREEN_INPUT_REPORT_SIZE + 1) {
      ESP_LOGW(TAG, "Touch report len error, expect %d, got %d",
               TOUCH_SCREEN_INPUT_REPORT_SIZE + 1, len);
      return ESP_FAIL;
    }
    ENQUEUE_PAYLOAD(payload, len);
    break;
  }

  default:
    ESP_LOGW(TAG, "Unknown command: 0x%02X", cmd);
    break;
  }
  return ESP_OK;
}

static void process_uart_buff() {
  if (rx_buffer_len > 0) {
#if DEBUG
    printf("接收数据[%d]: ", rx_buffer_len);
    for (int i = 0; i < rx_buffer_len; i++) {
      printf("%02x ", rx_buffer[i]);
    }
    printf("]\n");
#endif
    for (int i = 0; i < rx_buffer_len; i++) {
      uint8_t byte = rx_buffer[i];
      switch (state) {
      case STATE_WAIT_H1:
        if (byte == 0x55)
          state = STATE_WAIT_H2;
        break;
      case STATE_WAIT_H2:
        state = (byte == 0xAA) ? STATE_WAIT_LEN : STATE_WAIT_H1;
        break;
      case STATE_WAIT_LEN:
        expected_len = byte;
        if (expected_len > 0) {
          checksum = byte;
          current_idx = 0;
          state = STATE_WAIT_PAYLOAD;
        } else {
          state = STATE_WAIT_H1;
        }
        break;
      case STATE_WAIT_PAYLOAD:
        payload_buf[current_idx++] = byte;
        checksum ^= byte;
        if (current_idx >= expected_len) {
          state = STATE_WAIT_CHECKSUM;
        }
        break;
      case STATE_WAIT_CHECKSUM:
        if (byte == checksum) {
          process_uart_payload(payload_buf, expected_len);
        } else {
          ESP_LOGE(TAG, "CRC Error: exp %02X, got %02X", checksum, byte);
        }
        state = STATE_WAIT_H1;
        break;
      }
    }
  }
}
static void uart_rx_task(void *arg) {
  int uart0_fd = fileno(stdin);
  if (uart0_fd < 0) {
    ESP_LOGE(TAG, "无法打开 UART0 VFS");
    vTaskDelete(NULL);
  }
  struct termios tios;
  tcgetattr(uart0_fd, &tios);
  tios.c_iflag &= ~(INLCR | ICRNL | IGNCR | IXON);
  tios.c_lflag &= ~(ICANON | ECHO | ECHOE | ISIG);
  tcsetattr(uart0_fd, TCSANOW, &tios);

#if EXTERNAL_CTRL_ENABLE
  int uart1_fd = open("/dev/uart/1", O_RDWR | O_NONBLOCK);
  if (uart1_fd < 0) {
    ESP_LOGE(TAG, "无法打开 UART1 VFS");
    vTaskDelete(NULL);
  }
  tcgetattr(uart1_fd, &tios);
  tios.c_iflag &= ~(INLCR | ICRNL | IGNCR | IXON);
  tios.c_lflag &= ~(ICANON | ECHO | ECHOE | ISIG);
  tcsetattr(uart1_fd, TCSANOW, &tios);
#endif

  ESP_LOGI(TAG, "Unified RX task [0x55AA Protocol] started");

  while (1) {
    fd_set readfds;
    int max_fd = uart0_fd;
    FD_ZERO(&readfds);
    FD_SET(uart0_fd, &readfds);

#if EXTERNAL_CTRL_ENABLE
    FD_SET(uart1_fd, &readfds);
    max_fd = uart1_fd;
#endif

    int current_udp_sock = udp_sock;
    if (current_udp_sock >= 0) {
      FD_SET(current_udp_sock, &readfds);
      if (current_udp_sock > max_fd)
        max_fd = current_udp_sock;
    }

    struct timeval tv = {.tv_sec = 0, .tv_usec = 10 * 1000};
    int s = select(max_fd + 1, &readfds, NULL, NULL, &tv);

    if (s > 0) {
      rx_buffer_len = -1;
      if (FD_ISSET(uart0_fd, &readfds)) {
        rx_buffer_len = read(uart0_fd, rx_buffer, MAX_PACK_SIZE);
        process_uart_buff();
      }
#if EXTERNAL_CTRL_ENABLE
      if (FD_ISSET(uart1_fd, &readfds)) {
        rx_buffer_len = read(uart1_fd, rx_buffer, MAX_PACK_SIZE);
        process_uart_buff();
      }
#endif
      if (current_udp_sock >= 0 && FD_ISSET(current_udp_sock, &readfds)) {
        int len = recvfrom(current_udp_sock, payload_buf, MAX_PACK_SIZE, 0,
                           NULL, NULL);
        if (len >= 4 && payload_buf[0] == 0x55 && payload_buf[1] == 0xAA) {
          uint8_t p_len = payload_buf[2];
          if (len == p_len + 4) {
            uint8_t cal_cs = p_len;
            for (int i = 0; i < p_len; i++)
              cal_cs ^= payload_buf[3 + i];
            if (cal_cs == payload_buf[len - 1]) {
              process_uart_payload(&payload_buf[3], p_len);
            }
          }
        }
      }
    }

    if (gpio_get_level(APP_BUTTON) == APP_BUTTON_DOWN) {
      vTaskDelay(pdMS_TO_TICKS(20));
      if (gpio_get_level(APP_BUTTON) == APP_BUTTON_DOWN) {
        while (gpio_get_level(APP_BUTTON) != APP_BUTTON_UP)
          vTaskDelay(1);
        test_hid();
      }
    }
  }
}

/********* USB 事件回调 ***************/

static void usb_rx_handler(const tusbh_rx_report_t *report, void *ctx) {
  (void)ctx;
  // ESP_LOGI(TAG, "RX: instance=%u report_id=%u type=%u len=%u",
  //          report->instance, report->report_id, report->report_type,
  //          report->len);
  // ESP_LOG_BUFFER_HEX(TAG, report->data, report->len);
  uint8_t cmd = report->data[0];
  if (cmd == CMD_WIFI_CONFIG) {
    process_uart_payload(report->data, report->len);
  } else {
    ESP_LOGE(TAG, "Unknown command: %02X , len=%u , data:", cmd, report->len);
    ESP_LOG_BUFFER_HEX(TAG, report->data, report->len);
  }
}

void app_main(void) {
  setup_led();
  setLED(0, 0, 0, LED_MASK_R | LED_MASK_G | LED_MASK_B);
  LED_red_warning();
  esp_err_t ret = nvs_flash_init();
  if (ret == ESP_ERR_NVS_NO_FREE_PAGES ||
      ret == ESP_ERR_NVS_NEW_VERSION_FOUND) {
    ESP_ERROR_CHECK(nvs_flash_erase());
    ret = nvs_flash_init();
  }
  ESP_ERROR_CHECK(ret);

  hid_queue = xQueueCreate(HID_QUEUE_DEPTH, HID_QUEUE_ITEM_SIZE);
  assert(hid_queue != NULL);

  /* GPIO */
  const gpio_config_t boot_button_config = {
      .pin_bit_mask = BIT64(APP_BUTTON),
      .mode = GPIO_MODE_INPUT,
      .intr_type = GPIO_INTR_DISABLE,
      .pull_up_en = true,
      .pull_down_en = false,
  };
  ESP_ERROR_CHECK(gpio_config(&boot_button_config));

  /* UART */
  uart_config_t uart_cfg = {
      .baud_rate = CTRL_BAUD_RATE,
      .data_bits = UART_DATA_8_BITS,
      .parity = UART_PARITY_DISABLE,
      .stop_bits = UART_STOP_BITS_1,
      .flow_ctrl = UART_HW_FLOWCTRL_DISABLE,
      .source_clk = UART_SCLK_XTAL,
  };

  usb_serial_jtag_driver_config_t cfg = USB_SERIAL_JTAG_DRIVER_CONFIG_DEFAULT();
  usb_serial_jtag_driver_install(&cfg);
  esp_vfs_usb_serial_jtag_use_driver();
  uart_param_config(UART_NUM_0, &uart_cfg);
  uart_driver_install(UART_NUM_0, 2048, 0, 0, NULL, 0);

#if EXTERNAL_CTRL_ENABLE
  uart_cfg.baud_rate = EXTERNAL_CTRL_UART_BAUD_RATE;
  uart_param_config(UART_NUM_1, &uart_cfg);
  uart_driver_install(UART_NUM_1, 2048, 0, 0, NULL, 0);
  uart_set_pin(UART_NUM_1, EXTERNAL_CTRL_UART_TX, EXTERNAL_CTRL_UART_RX,
               UART_PIN_NO_CHANGE, UART_PIN_NO_CHANGE);
  const char *init_msg = "uart open!\n";
  uart_write_bytes(UART_NUM_1, init_msg, strlen(init_msg));
#endif

  esp_vfs_dev_uart_register();
  esp_vfs_dev_uart_use_driver(UART_NUM_0);
#if EXTERNAL_CTRL_ENABLE
  esp_vfs_dev_uart_use_driver(UART_NUM_1);
#endif

  vTaskDelay(pdMS_TO_TICKS(50));

  ESP_LOGI(TAG, "HIDcon-Device v0.1.2-20260307");
  ESP_LOGI(TAG, "主项目地址:https://github.com/RiderLty/go-touch-mapper");
  ESP_LOGI(TAG, "HID-devce 代码 by: Sakura1. && RiderLty && wdfky");

  /* USB - 通过 tusb_helper 初始化 */
  tusbh_config_t tusbh_cfg = {
      .event_cb = usb_event_handler,
      .event_cb_ctx = NULL,
      .rx_cb = usb_rx_handler,
      .rx_cb_ctx = NULL,
  };
  tusbh_init(&tusbh_cfg);

  xTaskCreatePinnedToCore(hid_sender_task, "hid_sender", 4096, NULL,
                          configMAX_PRIORITIES - 2, NULL, 1);
  xTaskCreatePinnedToCore(uart_rx_task, "uart_rx", 4096, NULL,
                          configMAX_PRIORITIES - 2, NULL, 0);

  vTaskDelay(pdMS_TO_TICKS(1000));
#if WIFI_ENABLE
  char *ssid = nvs_helper_get_str("ssid");
  char *pass = nvs_helper_get_str("password");
  if (ssid == NULL || pass == NULL) {
    ESP_LOGE(TAG, "SSID or password not found in NVS");
    return;
  } else {
    udp_sock = wifi_init_and_udp_connect(&wifi_status, ssid, pass);
    if (udp_sock < 0) {
      ESP_LOGE(TAG, "Failed to connect to server");
    }
    free(ssid);
    free(pass);
  }
#endif
  vTaskDelay(pdMS_TO_TICKS(2000));
  // test_hid();
}