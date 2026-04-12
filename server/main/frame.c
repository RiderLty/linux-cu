/*
 * frame.c - 简单帧格式编解码
 * 帧格式: [0x55][0xAA][len_lo][len_hi][cmd(1B)][payload(N)]
 */

#include "frame.h"

/* ---- 编码 ---- */

uint16_t frame_encode(uint8_t *buf, uint8_t cmd,
                      const uint8_t *payload, uint16_t payload_len) {
    uint16_t data_len = 1 + payload_len; /* cmd + payload */

    buf[0] = FRAME_MAGIC1;
    buf[1] = FRAME_MAGIC2;
    buf[2] = data_len & 0xFF;
    buf[3] = (data_len >> 8) & 0xFF;
    buf[4] = cmd;
    if (payload_len > 0 && payload != NULL) {
        memcpy(&buf[5], payload, payload_len);
    }

    return FRAME_HEADER + data_len;
}

/* ---- 解码 ---- */

void frame_decoder_init(frame_decoder_t *dec) {
    dec->state = FRAME_STATE_MAGIC1;
    dec->data_len = 0;
    dec->pos = 0;
}

int frame_decoder_feed(frame_decoder_t *dec, const uint8_t *data, uint16_t len,
                       frame_t *out) {
    for (uint16_t i = 0; i < len; i++) {
        uint8_t b = data[i];

        switch (dec->state) {
        case FRAME_STATE_MAGIC1:
            if (b == FRAME_MAGIC1) {
                dec->state = FRAME_STATE_MAGIC2;
            }
            /* 非 0x55 字节丢弃（日志等非帧数据） */
            break;

        case FRAME_STATE_MAGIC2:
            if (b == FRAME_MAGIC2) {
                dec->state = FRAME_STATE_LEN1;
            } else {
                dec->state = FRAME_STATE_MAGIC1;
            }
            break;

        case FRAME_STATE_LEN1:
            dec->data_len = b;
            dec->state = FRAME_STATE_LEN2;
            break;

        case FRAME_STATE_LEN2:
            dec->data_len |= (uint16_t)b << 8;
            if (dec->data_len < 1 || dec->data_len > FRAME_MAX_DATA) {
                /* 无效长度，重置 */
                dec->state = FRAME_STATE_MAGIC1;
                break;
            }
            dec->pos = 0;
            dec->state = FRAME_STATE_DATA;
            break;

        case FRAME_STATE_DATA:
            dec->buf[dec->pos++] = b;
            if (dec->pos >= dec->data_len) {
                /* 完整帧 */
                out->cmd = dec->buf[0];
                uint16_t plen = dec->data_len - 1;
                if (plen > 0) {
                    memcpy(out->payload, &dec->buf[1], plen);
                }
                out->payload_len = plen;
                dec->state = FRAME_STATE_MAGIC1;
                return 1;
            }
            break;
        }
    }

    return 0;
}
