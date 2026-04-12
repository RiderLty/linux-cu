/*
 * tusb_helper.h - TinyUSB 设备封装
 * 支持动态描述符（从 Linux 接收）+ HID 设备仿真
 * USB OTG 口用于设备模拟，UART 串口用于与 Linux 上位机通信
 */

#ifndef TUSB_HELPER_H
#define TUSB_HELPER_H

#include <stdbool.h>
#include <stdint.h>
#include "commands.h"

#ifdef __cplusplus
extern "C" {
#endif

/* ---- 动态描述符存储 ---- */

typedef struct {
    /* 设备描述符 */
    uint8_t  device_desc[MAX_DEVICE_DESC_LEN];
    uint16_t device_desc_len;

    /* 配置描述符 (完整，含所有接口/端点) */
    uint8_t  config_desc[MAX_CONFIG_DESC_LEN];
    uint16_t config_desc_len;

    /* HID 报告描述符 (每个接口一个) */
    uint8_t  report_desc[MAX_HID_INTERFACES][MAX_REPORT_DESC_LEN];
    uint16_t report_desc_len[MAX_HID_INTERFACES];

    /* 字符串描述符 */
    uint8_t  string_desc[16][MAX_STRING_DESC_LEN];
    uint16_t string_desc_len[16];
    uint8_t  string_desc_count;

    /* HID 接口映射: report_desc 的接口编号 */
    uint8_t  hid_iface_num[MAX_HID_INTERFACES];
    uint8_t  hid_iface_count;

    /* 接收完成标志 */
    bool     device_desc_ready;
    bool     config_desc_ready;
    uint8_t  report_desc_ready_mask; /* 每个 bit 对应一个 HID 接口 */
} dynamic_descriptors_t;

/* ---- 初始化 ---- */

/*
 * 初始化状态，不安装 USB 驱动
 * USB 驱动在 tusbh_start_emulation() 时用真实描述符安装
 */
void tusbh_init(void);

/* ---- 描述符存储接口 ---- */

/* 存储设备描述符，返回 true 表示成功 */
bool desc_store_device(const uint8_t *data, uint16_t len);

/* 存储配置描述符 */
bool desc_store_config(const uint8_t *data, uint16_t len);

/* 存储 HID 报告描述符 (iface_num 用于索引) */
bool desc_store_report(uint8_t iface_num, const uint8_t *data, uint16_t len);

/* 检查所有描述符是否就绪 */
bool desc_all_ready(void);

/* 获取动态描述符引用 */
const dynamic_descriptors_t *desc_get(void);

/* 启动仿真: 重新配置 USB 设备，添加 HID 接口 */
bool tusbh_start_emulation(void);

/* 停止仿真: 清除仿真状态 */
void tusbh_stop_emulation(void);

/* ---- HID 发送接口 ---- */

/* 向主机发送 HID 报表 */
bool tusbh_send_hid_report(uint8_t iface_num, const uint8_t *report, uint16_t len);

/* ---- 控制传输转发 ---- */

/*
 * 控制传输请求信息，用于转发给 Linux
 */
typedef struct {
    uint8_t  bm_request_type;
    uint8_t  b_request;
    uint16_t w_value;
    uint16_t w_index;
    uint16_t w_length;
    uint8_t  data_phase[256]; /* OUT 传输的数据 */
    uint16_t data_phase_len;
    bool     has_data_phase;  /* 是否有 data phase (OUT) */
} ctrl_request_t;

/*
 * 完成控制传输响应 (从 Linux 收到 CTRL_DATA 后调用)
 */
void ctrl_transfer_complete(const uint8_t *data, uint16_t len);

/*
 * 完成控制传输 STALL (从 Linux 收到 CTRL_STALL 后调用)
 */
void ctrl_transfer_stall(void);

/*
 * 设置控制传输请求回调
 * 当 P4 收到需要转发的控制传输时调用
 */
typedef void (*ctrl_req_cb_t)(const ctrl_request_t *req, void *ctx);
void ctrl_set_callback(ctrl_req_cb_t cb, void *ctx);

#ifdef __cplusplus
}
#endif

#endif /* TUSB_HELPER_H */
