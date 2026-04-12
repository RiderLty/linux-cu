#include "nvs.h"
#include "esp_err.h"
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#define STORAGE_NAMESPACE "network_conf"

/**
 * @brief 存储字符串到 NVS
 * @param key   键名 (最大 15 个字符)
 * @param value 字符串内容
 * @return esp_err_t 成功返回 ESP_OK
 */
esp_err_t nvs_helper_set_str(const char *key, const char *value);

/**
 * @brief 从 NVS 读取字符串
 * @param key 键名
 * @return char* 成功则返回动态分配的字符串指针（需手动 free），不存在则返回 NULL
 */
char *nvs_helper_get_str(const char *key);