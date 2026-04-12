#include "nvs_helper.h"
#include "esp_log.h"

static const char *TAG = "HIDcon-Device";

/**
 * @brief 存储字符串到 NVS
 * @param key   键名 (最大 15 个字符)
 * @param value 字符串内容
 * @return esp_err_t 成功返回 ESP_OK
 */
esp_err_t nvs_helper_set_str(const char *key, const char *value) {
  nvs_handle_t handle;
  esp_err_t err;

  // 1. 打开 NVS
  err = nvs_open(STORAGE_NAMESPACE, NVS_READWRITE, &handle);
  if (err != ESP_OK) {
    ESP_LOGE(TAG, "Error opening NVS: %s", esp_err_to_name(err));
    return err;
  }

  // 2. 写入字符串
  err = nvs_set_str(handle, key, value);
  if (err == ESP_OK) {
    // 3. 提交更改
    err = nvs_commit(handle);
  }

  nvs_close(handle);
  return err;
}

/**
 * @brief 从 NVS 读取字符串
 * @param key 键名
 * @return char* 成功则返回动态分配的字符串指针（需手动 free），不存在则返回 NULL
 */
char *nvs_helper_get_str(const char *key) {
  nvs_handle_t handle;
  esp_err_t err;

  // 1. 打开 NVS (只读)
  err = nvs_open(STORAGE_NAMESPACE, NVS_READONLY, &handle);
  if (err != ESP_OK)
    return NULL;

  size_t required_size;
  // 2. 获取存储的字符串长度
  err = nvs_get_str(handle, key, NULL, &required_size);
  if (err != ESP_OK) {
    // 如果 err == ESP_ERR_NVS_NOT_FOUND，表示 Key 不存在
    nvs_close(handle);
    return NULL;
  }

  // 3. 分配内存并读取
  char *buffer = malloc(required_size);
  if (buffer) {
    err = nvs_get_str(handle, key, buffer, &required_size);
    if (err != ESP_OK) {
      free(buffer);
      buffer = NULL;
    }
  }

  nvs_close(handle);
  return buffer;
}