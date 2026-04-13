/*
 * tusb_helper.c - TinyUSB 设备封装
 *
 * USB OTG 口用于设备模拟 (连接目标主机)
 * UART 串口用于与 Linux 上位机通信 (见 serial.c)
 *
 * 两阶段初始化:
 * 阶段1: tusbh_init() - 初始化状态，不安装 USB 驱动
 * 阶段2: tusbh_start_emulation() - 收到描述符后安装 USB 驱动
 */

#include "tusb_helper.h"
#include "class/hid/hid.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "tinyusb.h"
#include <string.h>

static const char *TAG = "tusb_helper";

/* 辅助宏 */
#define U16_LOW(u)  ((uint8_t)((u) & 0xFF))
#define U16_HIGH(u) ((uint8_t)(((u) >> 8) & 0xFF))

/* ---- 内部状态 ---- */
static dynamic_descriptors_t s_desc;
static volatile bool s_usb_ready = false;
static volatile bool s_emulating = false;

/* 控制传输转发 */
static ctrl_req_cb_t s_ctrl_cb = NULL;
static void         *s_ctrl_ctx = NULL;
static SemaphoreHandle_t s_ctrl_sem = NULL;
static volatile bool     s_ctrl_stall_flag = false;
static uint8_t  s_ctrl_resp_buf[256];
static uint16_t s_ctrl_resp_len = 0;

/* 待处理的控制传输 SETUP 包 */
static volatile bool s_ctrl_pending = false;
static uint8_t  s_ctrl_pending_bmrt;
static uint8_t  s_ctrl_pending_breq;
static uint16_t s_ctrl_pending_wval;
static uint16_t s_ctrl_pending_widx;
static uint16_t s_ctrl_pending_wlen;
static uint8_t  s_ctrl_pending_rhport;

/* ---- USB 字符串描述符 ---- */
static const char *s_str_desc_arr[] = {
    (const char[]){0x09, 0x04},  /* 0: English (US) */
    "Linux-CU",                   /* 1: Manufacturer */
    "USB HID Passthrough",        /* 2: Product */
    "000001",                     /* 3: Serial */
};

/* ---- 组合配置描述符 ---- */
static uint8_t s_composite_config_buf[2048];
static uint16_t s_composite_config_len = 0;

static void build_composite_config(void) {
    /* 使用已接收的完整配置描述符 */
    if (s_desc.config_desc_ready && s_desc.config_desc_len > 0) {
        memcpy(s_composite_config_buf, s_desc.config_desc, s_desc.config_desc_len);
        s_composite_config_len = s_desc.config_desc_len;
        return;
    }

    /* 不应该到达此处 - 描述符未就绪 */
    ESP_LOGE(TAG, "config descriptor not ready!");
    s_composite_config_len = 0;
}

/*
 * 设备/配置/字符串描述符回调由 esp_tinyusb 组件提供
 * 通过 tinyusb_config_t.descriptor 配置结构传入
 */

/* ---- HID 回调 ---- */

uint8_t const *tud_hid_descriptor_report_cb(uint8_t instance) {
    if (instance < s_desc.hid_iface_count) {
        return s_desc.report_desc[instance];
    }
    return NULL;
}

void tud_hid_report_complete_cb(uint8_t instance, uint8_t const *report, uint16_t len) {
    (void)instance;
    (void)report;
    (void)len;
}

uint16_t tud_hid_get_report_cb(uint8_t instance, uint8_t report_id,
                               hid_report_type_t report_type, uint8_t *buffer,
                               uint16_t reqlen) {
    (void)instance;
    (void)report_id;
    (void)report_type;
    (void)buffer;
    (void)reqlen;
    return 0;
}

void tud_hid_set_report_cb(uint8_t instance, uint8_t report_id,
                           hid_report_type_t report_type,
                           uint8_t const *buffer, uint16_t bufsize) {
    (void)instance;
    (void)report_id;
    (void)report_type;
    (void)buffer;
    (void)bufsize;
    /* SET_REPORT 由 tinyUSB 直接 ACK，暂不转发 */
}

/* ---- 控制传输拦截 ---- */

/* 标准请求可以本地处理，其他请求转发给 Linux */
static bool is_standard_get_descriptor(uint8_t bmrt, uint8_t breq) {
    return (bmrt & 0x60) == 0x00 && breq == TUSB_REQ_GET_DESCRIPTOR;
}

bool tud_control_xfer_cb(uint8_t rhport, uint8_t stage,
                         tusb_control_request_t const *request) {
    /* SETUP stage */
    if (stage == CONTROL_STAGE_SETUP) {
        uint8_t bmrt = request->bmRequestType;
        uint8_t breq = request->bRequest;

        /* 标准 GET_DESCRIPTOR: 本地处理 */
        if (is_standard_get_descriptor(bmrt, breq)) {
            return false; /* 让 tinyUSB 默认处理 */
        }

        /* 标准 SET_CONFIGURATION: 本地处理 */
        if ((bmrt & 0x60) == 0x00 && breq == TUSB_REQ_SET_CONFIGURATION) {
            return false;
        }

        /* 非标准请求: 转发给 Linux */
        if (s_emulating && s_ctrl_cb) {
            /* 保存 SETUP 包 */
            s_ctrl_pending_bmrt = bmrt;
            s_ctrl_pending_breq = breq;
            s_ctrl_pending_wval = request->wValue;
            s_ctrl_pending_widx = request->wIndex;
            s_ctrl_pending_wlen = request->wLength;
            s_ctrl_pending_rhport = rhport;
            s_ctrl_pending = true;

            /* 回调通知 main.c 发送 CMD_CTRL_REQ */
            ctrl_request_t creq = {
                .bm_request_type = bmrt,
                .b_request = breq,
                .w_value = request->wValue,
                .w_index = request->wIndex,
                .w_length = request->wLength,
                .data_phase_len = 0,
                .has_data_phase = false,
            };
            s_ctrl_cb(&creq, s_ctrl_ctx);

            /* 返回 true: 我们在 DATA/ACK stage 处理 */
            return true;
        }

        /* 未在仿真模式 - STALL */
        return false;
    }

    /* DATA stage / ACK stage - 我们处理的转发请求 */
    if (stage == CONTROL_STAGE_DATA && s_ctrl_pending) {
        /* 等待 Linux 响应 */
        if (xSemaphoreTake(s_ctrl_sem, pdMS_TO_TICKS(CTRL_TIMEOUT_MS)) == pdTRUE) {
            s_ctrl_pending = false;
            if (s_ctrl_stall_flag) {
                return false; /* STALL */
            }
            /* 有数据返回 - 对于 IN 请求，tinyUSB 期望我们调用 tud_control_xfer */
            bool is_in = (s_ctrl_pending_bmrt & 0x80) != 0;
            if (is_in && s_ctrl_resp_len > 0) {
                /* 构建临时 request 结构 */
                tusb_control_request_t resp_req = {
                    .bmRequestType = s_ctrl_pending_bmrt,
                    .bRequest = s_ctrl_pending_breq,
                    .wValue = s_ctrl_pending_wval,
                    .wIndex = s_ctrl_pending_widx,
                    .wLength = s_ctrl_pending_wlen,
                };
                tud_control_xfer(s_ctrl_pending_rhport, &resp_req,
                                s_ctrl_resp_buf, s_ctrl_resp_len);
            }
            return true;
        }
        /* 超时 */
        s_ctrl_pending = false;
        ESP_LOGW(TAG, "ctrl transfer timeout, STALL");
        return false;
    }

    /* ACK stage */
    if (stage == CONTROL_STAGE_ACK && s_ctrl_pending) {
        if (xSemaphoreTake(s_ctrl_sem, pdMS_TO_TICKS(CTRL_TIMEOUT_MS)) == pdTRUE) {
            s_ctrl_pending = false;
            return !s_ctrl_stall_flag;
        }
        s_ctrl_pending = false;
        return false;
    }

    return true;
}

/* ---- TinyUSB 事件 ---- */

static void tinyusb_event_cb(tinyusb_event_t *event, void *arg) {
    (void)arg;
    switch (event->id) {
    case TINYUSB_EVENT_ATTACHED:
        ESP_LOGI(TAG, "USB connected");
        s_usb_ready = true;
        break;
    case TINYUSB_EVENT_DETACHED:
        ESP_LOGI(TAG, "USB disconnected");
        s_usb_ready = false;
        break;
    default:
        break;
    }
}

/* ---- 公开接口 ---- */

void tusbh_init(void) {
    memset(&s_desc, 0, sizeof(s_desc));
    s_ctrl_sem = xSemaphoreCreateBinary();
    assert(s_ctrl_sem != NULL);

    /* USB 驱动不在此处安装，等待 tusbh_start_emulation() 用真实描述符安装 */
    ESP_LOGI(TAG, "tusb_helper initialized, waiting for descriptors...");
}

bool desc_store_device(const uint8_t *data, uint16_t len) {
    if (len > MAX_DEVICE_DESC_LEN || len == 0) return false;
    memcpy(s_desc.device_desc, data, len);
    s_desc.device_desc_len = len;
    s_desc.device_desc_ready = true;
    ESP_LOGI(TAG, "device descriptor stored: %d bytes", len);
    return true;
}

bool desc_store_config(const uint8_t *data, uint16_t len) {
    if (len > MAX_CONFIG_DESC_LEN || len == 0) return false;
    memcpy(s_desc.config_desc, data, len);
    s_desc.config_desc_len = len;
    s_desc.config_desc_ready = true;

    /* 解析配置描述符，提取 HID 接口信息 */
    s_desc.hid_iface_count = 0;
    uint16_t offset = 0;
    while (offset + 2 <= len) {
        uint8_t desc_len = data[offset];
        uint8_t desc_type = data[offset + 1];
        if (desc_len < 2) break;
        if (offset + desc_len > len) break;

        if (desc_type == TUSB_DESC_INTERFACE && desc_len >= 9) {
            uint8_t iface_class = data[offset + 5];
            uint8_t iface_num = data[offset + 2];
            if (iface_class == TUSB_CLASS_HID) {
                if (s_desc.hid_iface_count < MAX_HID_INTERFACES) {
                    s_desc.hid_iface_num[s_desc.hid_iface_count] = iface_num;
                    s_desc.hid_iface_count++;
                    ESP_LOGI(TAG, "  HID interface #%d found", iface_num);
                }
            }
        }
        offset += desc_len;
    }

    ESP_LOGI(TAG, "config descriptor stored: %d bytes, %d HID interfaces",
             len, s_desc.hid_iface_count);
    return true;
}

bool desc_store_report(uint8_t iface_num, const uint8_t *data, uint16_t len) {
    if (len > MAX_REPORT_DESC_LEN || len == 0) return false;

    int idx = -1;
    for (uint8_t i = 0; i < s_desc.hid_iface_count; i++) {
        if (s_desc.hid_iface_num[i] == iface_num) {
            idx = i;
            break;
        }
    }
    if (idx < 0 && s_desc.hid_iface_count < MAX_HID_INTERFACES) {
        idx = s_desc.hid_iface_count;
        s_desc.hid_iface_num[idx] = iface_num;
        s_desc.hid_iface_count++;
    }
    if (idx < 0) return false;

    memcpy(s_desc.report_desc[idx], data, len);
    s_desc.report_desc_len[idx] = len;
    s_desc.report_desc_ready_mask |= (1 << idx);
    ESP_LOGI(TAG, "report descriptor stored: iface=%d, %d bytes", iface_num, len);
    return true;
}

bool desc_all_ready(void) {
    if (!s_desc.device_desc_ready) return false;
    if (!s_desc.config_desc_ready) return false;
    for (uint8_t i = 0; i < s_desc.hid_iface_count; i++) {
        if (!(s_desc.report_desc_ready_mask & (1 << i))) return false;
    }
    return true;
}

const dynamic_descriptors_t *desc_get(void) {
    return &s_desc;
}

bool tusbh_start_emulation(void) {
    if (!desc_all_ready()) {
        ESP_LOGW(TAG, "descriptors not ready");
        return false;
    }

    build_composite_config();
    ESP_LOGI(TAG, "composite config descriptor: %d bytes", s_composite_config_len);

    /* 如果之前有驱动安装，先卸载 */
    esp_err_t err = tinyusb_driver_uninstall();
    if (err != ESP_OK) {
        ESP_LOGD(TAG, "uninstall previous driver: %s (may be first install)", esp_err_to_name(err));
    }

    vTaskDelay(pdMS_TO_TICKS(100));

    /* 重新安装，使用目标设备的完整描述符 */
    tinyusb_config_t tusb_cfg = {
        .port = TINYUSB_PORT_HIGH_SPEED_0,
        .phy = { .skip_setup = false, .self_powered = false, .vbus_monitor_io = -1 },
        .task = { .size = 4096, .priority = 5, .xCoreID = 1 },
        .descriptor = {
            .device = (s_desc.device_desc_ready && s_desc.device_desc_len == 18) ?
                      (const tusb_desc_device_t *)s_desc.device_desc : NULL,
            .qualifier = NULL,
            .string = (const char **)s_str_desc_arr,
            .string_count = sizeof(s_str_desc_arr) / sizeof(s_str_desc_arr[0]),
            .full_speed_config = s_composite_config_buf,
            .high_speed_config = s_composite_config_buf,
        },
        .event_cb = tinyusb_event_cb,
    };

    ESP_LOGI(TAG, "reinstalling with composite descriptor...");
    err = tinyusb_driver_install(&tusb_cfg);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "reinstall failed: %s", esp_err_to_name(err));
        return false;
    }

    s_emulating = true;
    ESP_LOGI(TAG, "emulation mode enabled, HID interfaces active");
    return true;
}

void tusbh_stop_emulation(void) {
    s_emulating = false;
}

bool tusbh_send_hid_report(uint8_t iface_num, const uint8_t *report, uint16_t len) {
    if (!s_usb_ready || !s_emulating) return false;
    for (uint8_t i = 0; i < s_desc.hid_iface_count; i++) {
        if (s_desc.hid_iface_num[i] == iface_num) {
            return tud_hid_n_report(i, 0, report, len);
        }
    }
    return false;
}

void ctrl_transfer_complete(const uint8_t *data, uint16_t len) {
    if (len > sizeof(s_ctrl_resp_buf)) len = sizeof(s_ctrl_resp_buf);
    memcpy(s_ctrl_resp_buf, data, len);
    s_ctrl_resp_len = len;
    s_ctrl_stall_flag = false;
    xSemaphoreGive(s_ctrl_sem);
}

void ctrl_transfer_stall(void) {
    s_ctrl_stall_flag = true;
    s_ctrl_resp_len = 0;
    xSemaphoreGive(s_ctrl_sem);
}

void ctrl_set_callback(ctrl_req_cb_t cb, void *ctx) {
    s_ctrl_cb = cb;
    s_ctrl_ctx = ctx;
}

/* ---- Dummy class driver: 接受所有未知接口 ---- */
/*
 * tinyUSB 的 process_set_config() 遍历配置描述符中所有接口，
 * 尝试为每个接口找到匹配的类驱动。如果找不到，SET_CONFIGURATION 失败。
 * 此 dummy 驱动接受所有不被内置驱动（HID/CDC/MSC）处理的接口，
 * 使包含 Audio 等非标准接口的设备（如 DS5 手柄）能正常枚举。
 */
#include "device/usbd_pvt.h"
#include "common/tusb_private.h"

static void     dummy_init(void) {}
static bool     dummy_deinit(void) { return true; }
static void     dummy_reset(uint8_t rhport) { (void)rhport; }

static uint16_t dummy_open(uint8_t rhport, tusb_desc_interface_t const *desc_itf, uint16_t max_len) {
    (void)rhport;
    // 跳过内置驱动已处理的接口类
    uint8_t cls = desc_itf->bInterfaceClass;
    if (cls == TUSB_CLASS_HID ||           // 0x03 - HID 驱动处理
        cls == TUSB_CLASS_CDC ||           // 0x02 - CDC 驱动处理
        cls == TUSB_CLASS_CDC_DATA ||      // 0x0A - CDC Data 驱动处理
        cls == TUSB_CLASS_MSC ||           // 0x08 - MSC 驱动处理
        cls == TUSB_CLASS_VENDOR_SPECIFIC) // 0xFF - Vendor 驱动处理
    {
        return 0;
    }
    // Audio MIDI (class=0x01, subclass=0x03) 由 MIDI 驱动处理
    if (cls == TUSB_CLASS_AUDIO && desc_itf->bInterfaceSubClass == 0x03) {
        return 0;
    }

    // 接受所有其他接口（Audio Control, Audio Streaming 等）
    uint16_t len = tu_desc_get_interface_total_len(desc_itf, 1, max_len);
    if (len >= sizeof(tusb_desc_interface_t)) {
        ESP_LOGD(TAG, "dummy: accepted iface %u class=0x%02X (%u bytes)",
                 desc_itf->bInterfaceNumber, cls, len);
        return len;
    }
    return 0;
}

static bool dummy_control_xfer_cb(uint8_t rhport, uint8_t stage, tusb_control_request_t const *request) {
    (void)rhport; (void)stage; (void)request;
    return false;
}

static bool dummy_xfer_cb(uint8_t rhport, uint8_t ep_addr, xfer_result_t result, uint32_t xferred_bytes) {
    (void)rhport; (void)ep_addr; (void)result; (void)xferred_bytes;
    return false;
}

static const usbd_class_driver_t s_dummy_driver = {
    .name            = "PASSTHRU",
    .init            = dummy_init,
    .deinit          = dummy_deinit,
    .reset           = dummy_reset,
    .open            = dummy_open,
    .control_xfer_cb = dummy_control_xfer_cb,
    .xfer_cb         = dummy_xfer_cb,
    .xfer_isr        = NULL,
    .sof             = NULL,
};

/* 覆盖 weak 实现，注册 dummy 驱动 */
usbd_class_driver_t const *usbd_app_driver_get_cb(uint8_t *driver_count) {
    *driver_count = 1;
    return &s_dummy_driver;
}
