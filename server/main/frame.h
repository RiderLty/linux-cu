/*
 * frame.h - 简单帧格式
 * [0x55][0xAA][len_lo][len_hi][cmd(1B)][payload(N)]
 */

#ifndef FRAME_H
#define FRAME_H

#include <stdint.h>
#include <stddef.h>
#include <string.h>

/* ---- 帧头 ---- */
#define FRAME_MAGIC1  0x55
#define FRAME_MAGIC2  0xAA
#define FRAME_HEADER  4       /* magic1 + magic2 + len(2) */
#define FRAME_MAX_DATA 4095   /* cmd + payload 最大长度 */

/* ---- 帧结构体 ---- */
typedef struct {
    uint8_t  cmd;
    uint8_t  payload[FRAME_MAX_DATA - 1];
    uint16_t payload_len;  /* payload 实际长度 */
} frame_t;

/* ---- 解码器状态 ---- */
typedef enum {
    FRAME_STATE_MAGIC1,
    FRAME_STATE_MAGIC2,
    FRAME_STATE_LEN1,
    FRAME_STATE_LEN2,
    FRAME_STATE_DATA
} frame_decoder_state_t;

typedef struct {
    frame_decoder_state_t state;
    uint16_t data_len;    /* cmd + payload 总长度 */
    uint16_t pos;         /* 已读取的数据字节数 */
    uint8_t  buf[FRAME_MAX_DATA];
} frame_decoder_t;

/* ---- 函数接口 ---- */

/* 初始化解码器 */
void frame_decoder_init(frame_decoder_t *dec);

/*
 * 喂入原始字节，输出解析出的一帧
 * 返回值: 1=有一帧输出, 0=无帧（需要更多数据）
 */
int frame_decoder_feed(frame_decoder_t *dec, const uint8_t *data, uint16_t len,
                       frame_t *out);

/* 编码一帧到 buf，返回总长度 */
uint16_t frame_encode(uint8_t *buf, uint8_t cmd,
                      const uint8_t *payload, uint16_t payload_len);

#endif /* FRAME_H */
