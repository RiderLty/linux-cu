/*
 * main.c - USB HID 透传仿真 ESP32-P4 固件入口
 *
 * 协议状态机: IDLE -> CONNECTED -> BOUND -> DESC_READY -> EMULATING
 *
 * 握手流程: Go 发送 CMD_PING -> P4 返回 CMD_PONG + magic number
 */

#include "commands.h"
#include "frame.h"
#include "led_helper.h"
#include "serial.h"
#include "tusb_helper.h"

#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs_flash.h"
#include <string.h>

static const char *TAG = "main";

/* ---- 固件信息 ---- */
#define FW_VERSION   "1.0.0"
#define FW_NAME      "usb-hid-passthrough"
#define FW_MAGIC     "P4OK"  /* 握手 magic number */

/* ---- 协议状态 ---- */
typedef enum {
    STATE_IDLE,       /* 等待 PING */
    STATE_CONNECTED,  /* 已握手，等待 BIND */
    STATE_BOUND,      /* 已绑定，等待描述符 */
    STATE_DESC_READY, /* 描述符就绪，等待 START */
    STATE_EMULATING,  /* 正在仿真 */
} proto_state_t;

static volatile proto_state_t s_state = STATE_IDLE;

/* 绑定信息 */
static uint16_t s_bound_vid = 0;
static uint16_t s_bound_pid = 0;

/* ---- 描述符组装 ---- */

typedef struct {
    uint8_t  desc_id;
    uint8_t  data[4096];
    uint16_t len;
    uint8_t  chunks_total;
    uint8_t  chunks_received;
    bool     complete;
} desc_entry_t;

#define MAX_DESC_ENTRIES 16
static desc_entry_t s_desc_entries[MAX_DESC_ENTRIES];
static int s_desc_entry_count = 0;

static desc_entry_t *get_or_create_entry(uint8_t desc_id) {
    for (int i = 0; i < s_desc_entry_count; i++) {
        if (s_desc_entries[i].desc_id == desc_id) {
            return &s_desc_entries[i];
        }
    }
    if (s_desc_entry_count >= MAX_DESC_ENTRIES) return NULL;
    desc_entry_t *e = &s_desc_entries[s_desc_entry_count++];
    memset(e, 0, sizeof(*e));
    e->desc_id = desc_id;
    return e;
}

static void reset_desc_entries(void) {
    s_desc_entry_count = 0;
    memset(s_desc_entries, 0, sizeof(s_desc_entries));
}

/*
 * 处理 DESC_BUNDLE 帧
 * 返回: 0=继续接收, 1=所有描述符已就绪, -1=错误
 */
static int handle_desc_bundle(const frame_t *frame) {
    if (frame->payload_len < 3) return -1;

    uint8_t desc_id = frame->payload[0];
    uint8_t chunk_idx = frame->payload[1];
    uint8_t chunk_total = frame->payload[2];
    const uint8_t *chunk_data = &frame->payload[3];
    uint16_t chunk_len = frame->payload_len - 3;

    desc_entry_t *entry = get_or_create_entry(desc_id);
    if (entry == NULL) return -1;

    entry->chunks_total = chunk_total;

    if (entry->len + chunk_len > sizeof(entry->data)) return -1;

    memcpy(&entry->data[entry->len], chunk_data, chunk_len);
    entry->len += chunk_len;
    entry->chunks_received++;

    if (entry->chunks_received >= entry->chunks_total) {
        entry->complete = true;
    }

    serial_send_frame(CMD_DESC_ACK, &desc_id, 1);

    bool has_device = false;
    bool has_config = false;
    bool all_complete = true;

    for (int i = 0; i < s_desc_entry_count; i++) {
        if (!s_desc_entries[i].complete) {
            all_complete = false;
        }
        if (s_desc_entries[i].desc_id == DESC_ID_DEVICE) has_device = true;
        if (s_desc_entries[i].desc_id == DESC_ID_CONFIG) has_config = true;
    }

    if (all_complete && has_device && has_config) {
        return 1;
    }
    return 0;
}

static void register_descriptors(void) {
    for (int i = 0; i < s_desc_entry_count; i++) {
        desc_entry_t *e = &s_desc_entries[i];
        if (!e->complete) continue;

        if (e->desc_id == DESC_ID_DEVICE) {
            desc_store_device(e->data, e->len);
        } else if (e->desc_id == DESC_ID_CONFIG) {
            desc_store_config(e->data, e->len);
        } else if (e->desc_id >= DESC_ID_REPORT_BASE && e->desc_id < DESC_ID_STRING_BASE) {
            uint8_t iface_num = e->desc_id - DESC_ID_REPORT_BASE;
            desc_store_report(iface_num, e->data, e->len);
        }
    }
}

/* ---- 控制传输转发 ---- */

static void ctrl_req_handler(const ctrl_request_t *req, void *ctx) {
    (void)ctx;

    uint8_t buf[8];
    buf[0] = req->bm_request_type;
    buf[1] = req->b_request;
    buf[2] = req->w_value & 0xFF;
    buf[3] = (req->w_value >> 8) & 0xFF;
    buf[4] = req->w_index & 0xFF;
    buf[5] = (req->w_index >> 8) & 0xFF;
    buf[6] = req->w_length & 0xFF;
    buf[7] = (req->w_length >> 8) & 0xFF;

    serial_send_frame(CMD_CTRL_REQ, buf, 8);
}

/* ---- 命令处理 ---- */

static void handle_ping(void) {
    /* 返回 PONG + magic number */
    serial_send_frame(CMD_PONG, (const uint8_t *)FW_MAGIC, strlen(FW_MAGIC));
    if (s_state == STATE_IDLE) {
        s_state = STATE_CONNECTED;
        setLED(255, 255, 0, LED_MASK_R | LED_MASK_G);
    }
}

static void handle_get_info(void) {
    char info[64];
    snprintf(info, sizeof(info), "%s v%s (HID=%d)",
             FW_NAME, FW_VERSION, MAX_HID_INTERFACES);
    serial_send_frame(CMD_INFO_RESP, (uint8_t *)info, strlen(info));
}

static void handle_bind_device(const frame_t *frame) {
    if (s_state != STATE_CONNECTED) {
        uint8_t err[] = {CMD_BIND_DEVICE, ERR_STATE};
        serial_send_frame(CMD_ERROR, err, 2);
        return;
    }

    if (frame->payload_len < 4) {
        uint8_t err[] = {CMD_BIND_DEVICE, ERR_FRAME};
        serial_send_frame(CMD_ERROR, err, 2);
        return;
    }

    s_bound_vid = (uint16_t)frame->payload[0] | ((uint16_t)frame->payload[1] << 8);
    s_bound_pid = (uint16_t)frame->payload[2] | ((uint16_t)frame->payload[3] << 8);

    reset_desc_entries();
    s_state = STATE_BOUND;
    serial_send_cmd(CMD_BIND_ACK);
}

static void on_desc_bundle(const frame_t *frame) {
    if (s_state != STATE_BOUND && s_state != STATE_DESC_READY) {
        uint8_t err[] = {CMD_DESC_BUNDLE, ERR_STATE};
        serial_send_frame(CMD_ERROR, err, 2);
        return;
    }

    int result = handle_desc_bundle(frame);
    if (result < 0) {
        uint8_t err[] = {CMD_DESC_BUNDLE, ERR_DESC};
        serial_send_frame(CMD_ERROR, err, 2);
    } else if (result == 1) {
        register_descriptors();
        if (desc_all_ready()) {
            s_state = STATE_DESC_READY;
        }
    }
}

static void handle_start_emul(void) {
    if (s_state != STATE_DESC_READY) {
        uint8_t err[] = {CMD_START_EMUL, ERR_STATE};
        serial_send_frame(CMD_ERROR, err, 2);
        return;
    }

    if (!tusbh_start_emulation()) {
        uint8_t err[] = {CMD_START_EMUL, ERR_USB};
        serial_send_frame(CMD_ERROR, err, 2);
        return;
    }

    s_state = STATE_EMULATING;
    serial_send_cmd(CMD_EMUL_ACK);
    setLED(0, 255, 0, LED_MASK_G);
}

static void handle_stop_emul(void) {
    if (s_state == STATE_EMULATING) {
        tusbh_stop_emulation();
        s_state = STATE_CONNECTED;
        setLED(255, 255, 0, LED_MASK_R | LED_MASK_G);
    }
    serial_send_cmd(CMD_STOP_ACK);
}

static void handle_hid_report(const frame_t *frame) {
    if (s_state != STATE_EMULATING) return;
    if (frame->payload_len < 2) return;

    uint8_t iface = frame->payload[0];
    uint8_t data_len = frame->payload[1];
    const uint8_t *data = &frame->payload[2];

    if (data_len > 0 && data_len <= frame->payload_len - 2) {
        tusbh_send_hid_report(iface, data, data_len);
    }
}

static void handle_ctrl_data(const frame_t *frame) {
    if (s_state != STATE_EMULATING) return;
    if (frame->payload_len < 2) return;

    uint16_t data_len = (uint16_t)frame->payload[0] | ((uint16_t)frame->payload[1] << 8);
    const uint8_t *data = &frame->payload[2];
    uint16_t actual = frame->payload_len - 2;
    if (actual < data_len) data_len = actual;

    ctrl_transfer_complete(data, data_len);
}

/* ---- 协议主循环 ---- */

static void protocol_task(void *arg) {
    (void)arg;
    frame_t frame;

    while (1) {
        serial_poll();

        if (serial_recv_frame_nowait(&frame)) {
            switch (frame.cmd) {
            case CMD_PING:       handle_ping(); break;
            case CMD_GET_INFO:   handle_get_info(); break;
            case CMD_BIND_DEVICE: handle_bind_device(&frame); break;
            case CMD_DESC_BUNDLE: on_desc_bundle(&frame); break;
            case CMD_START_EMUL: handle_start_emul(); break;
            case CMD_STOP_EMUL:  handle_stop_emul(); break;
            case CMD_HID_REPORT: handle_hid_report(&frame); break;
            case CMD_CTRL_DATA:  handle_ctrl_data(&frame); break;
            case CMD_CTRL_STALL:
                if (s_state == STATE_EMULATING) ctrl_transfer_stall();
                break;
            case CMD_RESET:
                s_state = STATE_IDLE;
                reset_desc_entries();
                setLED(255, 0, 0, LED_MASK_R);
                break;
            default:
                break;
            }
        }

        vTaskDelay(pdMS_TO_TICKS(10));
    }
}

/* ---- 入口 ---- */

void app_main(void) {
    /* 禁用系统日志 */
    esp_log_level_set("*", ESP_LOG_NONE);

    /* 初始化通信 (照抄 example.c:27 + 4M 波特率) */
    serial_config_t serial_cfg = {
        .tx_buffer_size = SERIAL_TX_BUF_SIZE,
        .rx_buffer_size = SERIAL_RX_BUF_SIZE,
    };
    serial_init(&serial_cfg);

    /* 初始化 NVS */
    esp_err_t ret = nvs_flash_init();
    if (ret == ESP_ERR_NVS_NO_FREE_PAGES || ret == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        ret = nvs_flash_init();
    }
    ESP_ERROR_CHECK(ret);

    /* 初始化 LED */
    setup_led();
    setLED(255, 0, 0, LED_MASK_R);

    /* 初始化 USB */
    tusbh_init();

    /* 设置控制传输回调 */
    ctrl_set_callback(ctrl_req_handler, NULL);

    /* 创建协议主循环任务 */
    xTaskCreatePinnedToCore(protocol_task, "proto", 16384, NULL, 6, NULL, 1);
}
