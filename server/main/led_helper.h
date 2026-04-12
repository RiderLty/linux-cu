#include "driver/gpio.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "led_strip.h"
#include <stdint.h>

#if CONFIG_IDF_TARGET_ESP32S3
#define BOARD_LED_GPIO GPIO_NUM_48
#elif CONFIG_IDF_TARGET_ESP32P4
#define BOARD_LED_GPIO GPIO_NUM_51
#else
#define BOARD_LED_GPIO GPIO_NUM_48
#endif

#define LED_MASK_R (0x01)
#define LED_MASK_G (0x02)
#define LED_MASK_B (0x04)

// WiFi status definitions
#define WIFI_CONNECTING 0
#define WIFI_CONNECTED 1
#define WIFI_CONNECT_FAILED 2

void setup_led(void);
void setLED(uint8_t r, uint8_t g, uint8_t b, uint8_t mask);
void LED_red_warning();
void LED_red_ok();
void LED_wifi_status(void *pvParameters);