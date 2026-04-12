#include <stdio.h>
#include "led_helper.h"
#include "esp_log.h"

static uint8_t R = 0;
static uint8_t G = 0;
static uint8_t B = 0;
static led_strip_handle_t led_strip;

void setup_led(void) {
  led_strip_config_t strip_config = {
      .strip_gpio_num = BOARD_LED_GPIO, // 关键：连接到 GPIO51
      .max_leds = 1,                    // 单颗 LED
      .led_model = LED_MODEL_WS2812,    // LED 型号
      .flags.invert_out = false,        // 信号电平不反转
  };
  led_strip_rmt_config_t rmt_config = {
#if ESP_IDF_VERSION >= ESP_IDF_VERSION_VAL(5, 0, 0)
      .clk_src = RMT_CLK_SRC_DEFAULT,    // 默认时钟源
      .resolution_hz = 10 * 1000 * 1000, // 10MHz 分辨率
      .flags.with_dma = false,           // 单颗 LED 无需 DMA
#endif
  };
  ESP_ERROR_CHECK(
      led_strip_new_rmt_device(&strip_config, &rmt_config, &led_strip));
  ESP_ERROR_CHECK(led_strip_clear(led_strip));
}

void setLED(uint8_t r, uint8_t g, uint8_t b,
            uint8_t mask) { // 设置LED颜色,如果参数为-1,则不改变该通道颜色
  if (mask & LED_MASK_R)
    R = r;
  if (mask & LED_MASK_G)
    G = g;
  if (mask & LED_MASK_B)
    B = b;
  ESP_ERROR_CHECK(led_strip_set_pixel(led_strip, 0, R, G, B));
  ESP_ERROR_CHECK(led_strip_refresh(led_strip));
}

void LED_red_warning() { setLED(24, 0, 0, LED_MASK_R); }
void LED_red_ok() {
  for (int i = 24; i >= 0; --i) {
    setLED(i, 0, 0, LED_MASK_R);
    vTaskDelay(pdMS_TO_TICKS(14));
  }
  vTaskDelete(NULL);
}

void LED_wifi_status(void *pvParameters) {
  // 将传入的参数强制转换为 uint8_t 指针
  uint8_t *p_status = (uint8_t *)pvParameters;

  if (p_status == NULL) {
    ESP_LOGE("HIDcon-Device", "LED_wifi_status: Pointer is NULL");
    vTaskDelete(NULL);
    return;
  }

  while (1) {
    // 通过解引用指针获取实时状态
    uint8_t current_status = *p_status;
    ESP_LOGI("HIDcon-Device", "wifi_status (via pointer): %d", current_status);

    switch (current_status) {
    case WIFI_CONNECTING:
      for (int i = 0; i < 3; ++i) {
        setLED(0, 20, 0, LED_MASK_G);
        vTaskDelay(pdMS_TO_TICKS(30));
        setLED(0, 0, 0, LED_MASK_G);
        vTaskDelay(pdMS_TO_TICKS(30));
      }
      vTaskDelay(pdMS_TO_TICKS(800));
      break; // 使用 break 跳出 switch 继续 while 循环

    case WIFI_CONNECTED:
      setLED(0, 20, 0, LED_MASK_G);
      vTaskDelay(pdMS_TO_TICKS(500));
      for (int i = 20; i >= 0; --i) {
        setLED(0, i, 0, LED_MASK_G);
        vTaskDelay(pdMS_TO_TICKS(14));
      }
      vTaskDelete(NULL); // 任务完成删除自己
      return;

    case WIFI_CONNECT_FAILED:
      for (int i = 0; i < 3; ++i) {
        setLED(0, 48, 0, LED_MASK_G);
        vTaskDelay(pdMS_TO_TICKS(300));
        setLED(0, 0, 0, LED_MASK_G);
        vTaskDelay(pdMS_TO_TICKS(300));
      }
      vTaskDelete(NULL);
      return;

    default:
      vTaskDelay(pdMS_TO_TICKS(100)); // 防止未知状态导致死循环占满CPU
      break;
    }
  }
}