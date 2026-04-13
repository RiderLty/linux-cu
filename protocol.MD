# USB HID 透传模拟系统 — 协议与架构规范

## 1. 系统概述

Linux 上位机（client，Golang）通过 libusb 读取任意 USB 设备的完整描述符和交互数据，通过 JTAG 串口透传给 ESP32-P4（server，ESP-IDF C）。P4 使用 tinyUSB 复现目标设备的行为，对主机而言等同于真实设备。

**核心原则：**
- 描述符透传：不解析描述符，Linux 读取的原始字节直接发送给 P4，P4 原样注册到 tinyUSB
- 控制传输全转发：P4 收到主机的控制传输请求后，通过串口转发给 Linux，Linux 用 libusb 转发给真实设备，响应原路返回
- 多对多拓扑：一个 Linux 实例可管理多个 P4，一个 P4 仅被一个 Linux 管理（通过绑定关系决定）

---

## 2. 项目架构

### 2.1 目录结构

```
linux-cu/
├── protocol.MD              # 本文件：协议与架构规范
├── README.md                # 项目说明
├── go.mod                   # Go 模块定义
├── go.sum                   # Go 依赖校验
├── client/                  # Linux 上位机端 (Golang)
│   ├── cmd/
│   │   ├── main.go          # 程序入口，CLI（start / list 子命令）
│   │   └── debug/
│   │       └── main.go      # 调试工具入口
│   └── pkg/
│       ├── serial/
│       │   ├── serial.go    # 串口通信（打开串口、读写 goroutine）
│       │   ├── frame.go     # 帧封装/解析状态机 (0x55 0xAA 格式)
│       │   └── frame_test.go # 帧编解码测试
│       ├── protocol/
│       │   ├── session.go   # 协议会话管理（SendAndWait、超时重试）
│       │   ├── commands.go  # 指令常量、错误码、超时参数
│       │   ├── types.go     # 指令 payload 结构体 (Marshal/Unmarshal)
│       │   └── types_test.go # payload 编解码测试
│       ├── session/
│       │   └── session.go   # 完整仿真生命周期管理
│       └── usb/
│           ├── descriptor.go # USB 描述符读取（libusb cgo）
│           ├── device.go    # USB 设备句柄管理
│           ├── transfer.go  # 控制传输代理
│           └── print.go     # 描述符格式化输出
│
├── server/                  # ESP32-P4 固件端 (ESP-IDF C)
│   ├── CMakeLists.txt       # 顶层 ESP-IDF 项目 CMake
│   └── main/
│       ├── CMakeLists.txt   # main 组件 CMake
│       ├── idf_component.yml # ESP-IDF 组件依赖
│       ├── main.c           # 程序入口，硬件初始化、协议主循环
│       ├── commands.h       # 指令常量、错误码定义
│       ├── frame.c          # 帧编解码 (0x55 0xAA 格式)
│       ├── frame.h
│       ├── serial.c         # 串口通信（VFS stdin/stdout 读写）
│       ├── serial.h
│       ├── tusb_helper.c    # TinyUSB 设备封装（动态描述符、HID、控制传输）
│       ├── tusb_helper.h
│       ├── led_helper.c     # LED 控制
│       ├── led_helper.h
│       ├── log_uart.c       # UART 日志转发
│       ├── log_uart.h
│       ├── nvs_helper.c     # NVS 存储辅助
│       └── nvs_helper.h
```

### 2.2 模块职责

| 模块 | Client (Golang) | Server (ESP-IDF C) |
|------|-----------------|-------------------|
| serial | 打开串口（go.bug.st/serial），帧的封装/解析（0x55 0xAA 格式） | USB-SERIAL-JTAG 直接读写，帧收发 |
| protocol | 指令打包、超时重传、会话管理 | 指令解析、响应生成（FreeRTOS task） |
| usb | libusb 设备枚举、描述符读取、控制传输代理（cgo） | — |
| session | 仿真生命周期管理（握手→绑定→描述符→仿真→心跳） | — |
| tusb_helper | — | 动态描述符存储、tinyUSB 设备驱动管理、HID 发送、控制传输转发 |
| main | CLI 入口（cobra：start / list 子命令） | 硬件初始化、协议主循环状态机 |

> **Golang 依赖：**
> - `go.bug.st/serial` — 跨平台串口通信
> - cgo 直接绑定 `libusb-1.0` — USB 设备访问
> - `github.com/spf13/cobra` — CLI 框架
>
> **ESP-IDF 依赖：**
> - `esp_tinyusb` — USB 设备栈（USB OTG 口连接目标主机）
> - `driver/usb_serial_jtag.h` — USB-SERIAL-JTAG 通信
> - FreeRTOS — 任务调度、队列、信号量 |

---

## 3. 串口帧格式

### 3.1 物理层参数

底层使用 USB-SERIAL-JTAG，无需配置波特率、数据位等参数。Go 端通过串口库（go.bug.st/serial）打开 `/dev/ttyACM*` 设备，ESP32-P4 端通过 `usb_serial_jtag_read/write_bytes` 直接读写。

> Go 端波特率参数不影响 USB-SERIAL-JTAG 实际通信速率。

### 3.2 帧结构

```
┌─────────┬─────────┬──────────┬───────────┬─────────────┐
│ MAGIC1  │ MAGIC2  │   LEN    │    CMD    │   PAYLOAD   │
│  0x55   │  0xAA   │ 2 bytes  │  1 byte   │  0~4094 B   │
└─────────┴─────────┴──────────┴───────────┴─────────────┘
```

| 字段 | 大小 | 说明 |
|------|------|------|
| MAGIC1 | 1 byte | 帧头第一字节，固定 `0x55` |
| MAGIC2 | 1 byte | 帧头第二字节，固定 `0xAA` |
| LEN | 2 bytes | CMD + PAYLOAD 的总长度（小端序），不含帧头和 LEN 自身 |
| CMD | 1 byte | 指令类型，见第 4 节 |
| PAYLOAD | 0~4094 bytes | 指令数据，长度由 LEN-1 决定 |

> 最大帧长：MAGIC(2) + LEN(2) + CMD(1) + PAYLOAD(4094) = 4099 bytes
> LEN 最大值：4095（cmd + payload）

### 3.3 帧同步

接收端状态机（无字节转义，无 CRC 校验）：

```
MAGIC1 → (收到 0x55) → MAGIC2 → (收到 0xAA) → LEN1 → LEN2 → DATA(LEN bytes)
                                                                      ↓
                                                              (收齐 → 输出帧)
                                                                      ↓
                                                              (回到 MAGIC1)
```

非帧数据（bootloader 日志等）在 MAGIC1 状态被丢弃，Go 端的 Decoder 会将非帧原始数据单独返回用于打印。

---

## 4. 指令集 (CMD)

### 4.1 指令分类

指令分为 **管理类**（Linux → P4）和 **转发类**（双向）两大类。

### 4.2 指令表

#### 管理类指令 (Linux → P4 主动发起)

| CMD | 名称 | 方向 | 说明 |
|-----|------|------|------|
| `0x01` | `CMD_PING` | L→P | 心跳/连接检测 |
| `0x02` | `CMD_PONG` | P→L | 心跳响应 |
| `0x10` | `CMD_GET_INFO` | L→P | 查询 P4 固件版本、能力 |
| `0x11` | `CMD_INFO_RESP` | P→L | 返回 P4 信息 |
| `0x20` | `CMD_BIND_DEVICE` | L→P | 绑定要模拟的设备，携带设备标识 |
| `0x21` | `CMD_BIND_ACK` | P→L | 确认绑定 |
| `0x30` | `CMD_DESC_BUNDLE` | L→P | 发送完整描述符数据包（见 4.3） |
| `0x31` | `CMD_DESC_ACK` | P→L | 描述符接收确认（含序号） |
| `0x40` | `CMD_START_EMUL` | L→P | 通知 P4 开始模拟设备 |
| `0x41` | `CMD_EMUL_ACK` | P→L | 确认开始模拟 |
| `0x50` | `CMD_STOP_EMUL` | L→P | 停止模拟 |
| `0x51` | `CMD_STOP_ACK` | P→L | 确认停止 |
| `0xFE` | `CMD_ERROR` | 双向 | 错误报告 |
| `0xFF` | `CMD_RESET` | L→P | 重置 P4 状态 |

#### 转发类指令（双向）

| CMD | 名称 | 方向 | 说明 |
|-----|------|------|------|
| `0x60` | `CMD_HID_REPORT` | L→P | Linux 转发真实设备的 HID 报表给 P4 |
| `0x70` | `CMD_CTRL_REQ` | P→L | P4 收到主机控制传输请求，转发给 Linux |
| `0x71` | `CMD_CTRL_DATA` | L→P | Linux 用 libusb 执行控制传输后，返回数据给 P4 |
| `0x72` | `CMD_CTRL_STALL` | L→P | Linux 告知 P4 对该请求执行 STALL |

### 4.3 描述符分包传输 (CMD_DESC_BUNDLE)

USB 设备描述符总量可能超过单帧最大长度（4092B），需要分包：

`CMD_DESC_BUNDLE` 的 PAYLOAD 格式：

```
┌──────────┬────────────┬──────────────┬─────────────────┐
│ DESC_ID  │ CHUNK_IDX  │ CHUNK_TOTAL  │ DESC_RAW_DATA   │
│  1 byte  │  1 byte    │   1 byte     │  0~4089 bytes   │
└──────────┴────────────┴──────────────┴─────────────────┘
```

| 字段 | 说明 |
|------|------|
| DESC_ID | 描述符标识，用于区分同一设备的不同描述符 |
| CHUNK_IDX | 当前分包索引（从 0 开始） |
| CHUNK_TOTAL | 总分包数 |
| DESC_RAW_DATA | 描述符原始字节片段 |

**DESC_ID 分配：**

| DESC_ID | 对应描述符 |
|---------|-----------|
| `0x01` | 设备描述符 (Device Descriptor) |
| `0x02` | 配置描述符 + 所有接口/端点描述符 (Configuration Bundle) |
| `0x10`~`0x1F` | HID 报告描述符 (Report Descriptor)，每个接口一个 ID |
| `0x20`~`0x2F` | 字符串描述符 (String Descriptor)，按索引分配 |

> P4 收到所有分包后按 DESC_ID 重组，全部描述符接收完成后回复 `CMD_DESC_ACK`。

### 4.4 HID 报表转发 (CMD_HID_REPORT)

```
┌────────────┬────────────┬──────────────┐
│ INTERFACE  │  DATA_LEN  │  REPORT_DATA │
│  1 byte    │  1 byte    │  0~255 bytes │
└────────────┴────────────┴──────────────┘
```

| 字段 | 说明 |
|------|------|
| INTERFACE | 目标接口编号（对应描述符中的 bInterfaceNumber） |
| DATA_LEN | 报表数据长度 |
| REPORT_DATA | 原始 HID 报表字节 |

### 4.5 控制传输转发

**CMD_CTRL_REQ (P4 → Linux)：** P4 收到主机的控制传输请求

```
┌─────────┬───────────┬──────────┬────────────┬─────────────┬──────────────┐
│ bmRequest│ bRequest  │ wValue   │  wIndex    │  wLength    │  DATA_PHASE │
│  1 byte  │  1 byte   │ 2 bytes  │  2 bytes   │  2 bytes    │  0~N bytes  │
│ (LE)     │ (LE)      │ (LE)     │ (LE)       │ (LE)        │             │
└─────────┴───────────┴──────────┴────────────┴─────────────┴──────────────┘
```

> 对于有 DATA Phase 的 OUT 控制传输（如 SET_REPORT），DATA_PHASE 携带主机下发的数据。
> 对于 IN 控制传输（如 GET_DESCRIPTOR），DATA_PHASE 为空。

**CMD_CTRL_DATA (Linux → P4)：** Linux 用 libusb 执行控制传输后的响应

```
┌────────────┬──────────────────┐
│  DATA_LEN  │   RESPONSE_DATA  │
│  2 bytes   │   0~N bytes      │
└────────────┴──────────────────┘
```

**CMD_CTRL_STALL (Linux → P4)：** Linux 告知 P4 该请求无法处理

```
（无 PAYLOAD）
```

> P4 收到 CMD_CTRL_STALL 后，对主机的控制传输返回 STALL。

---

## 5. 交互流程

### 5.1 完整时序图

```
  Linux (Client)                         ESP32-P4 (Server)
       |                                        |
       |  ---- CMD_PING ------------------>     |   连接检测
       |  <--- CMD_PONG + "P4OK" ---------     |
       |                                        |
       |  [Linux 本地通过 libusb 读取描述符]      |
       |                                        |
       |  ---- CMD_BIND_DEVICE ----------->     |   绑定目标设备 (VID/PID)
       |  <--- CMD_BIND_ACK ---------------     |
       |                                        |
       |  ---- CMD_DESC_BUNDLE ----------->     |   发送设备描述符
       |  ---- CMD_DESC_BUNDLE ----------->     |   (可能多帧分包)
       |  <--- CMD_DESC_ACK ---------------     |   P4 确认
       |                                        |
       |  ---- CMD_DESC_BUNDLE ----------->     |   发送配置描述符
       |  <--- CMD_DESC_ACK ---------------     |
       |                                        |
       |  ---- CMD_DESC_BUNDLE ----------->     |   发送 HID 报告描述符
       |  <--- CMD_DESC_ACK ---------------     |   (每个接口各发一次)
       |                                        |
       |  ---- CMD_START_EMUL ------------->    |   开始模拟
       |  <--- CMD_EMUL_ACK ---------------    |
       |                                        |
       |  ==================== 正常运行阶段 ====================
       |                                        |
       |  ---- CMD_HID_REPORT ------------->    |   转发真实设备报表
       |  ---- CMD_HID_REPORT ------------->    |
       |                                        |
       |                          主机连接 P4    |
       |                                        |
       |  <--- CMD_CTRL_REQ ---------------     |   主机发控制传输请求
       |  ---- CMD_CTRL_DATA -------------->    |   Linux 代理执行后返回
       |                                        |
       |  <--- CMD_CTRL_REQ ---------------     |
       |  ---- CMD_CTRL_STALL ------------->    |   无法处理，返回 STALL
       |                                        |
       |  ---- CMD_HID_REPORT ------------->    |   持续转发报表
       |                                        |
       |  ==================== 停止阶段 ========================
       |                                        |
       |  ---- CMD_STOP_EMUL ------------->     |   停止模拟
       |  <--- CMD_STOP_ACK ----------------    |
       |                                        |
```

### 5.2 连接建立阶段

1. Linux 发送 `CMD_PING`，P4 返回 `CMD_PONG` + magic number `"P4OK"`，超时 500ms，重试 3 次
2. Linux 通过 libusb 读取目标 USB 设备的完整描述符（本地读取，不通过串口）
3. Linux 发送 `CMD_BIND_DEVICE`，携带目标 USB 设备的 VID/PID 作为标识
4. P4 回复 `CMD_BIND_ACK`，进入 BOUND 状态

### 5.3 描述符传输阶段

1. Linux 按 DESC_ID 顺序发送描述符：设备描述符 → 配置描述符 → 各 HID 报告描述符 → 字符串描述符
2. 每个描述符可能需要多个 `CMD_DESC_BUNDLE` 帧分包传输
3. P4 收到每个 DESC_ID 的所有分包后，回复 `CMD_DESC_ACK`，携带确认的 DESC_ID
4. 所有描述符传输完成后，Linux 发送 `CMD_START_EMULATE`

### 5.4 设备模拟阶段

1. P4 根据收到的描述符，通过 tinyUSB 动态注册为 USB 设备
2. P4 连接主机（PC/游戏机等）后，主机枚举 P4（P4 使用传入的描述符响应）
3. Linux 持续读取真实设备的 HID 报表，通过 `CMD_HID_REPORT` 发给 P4
4. P4 收到报表后，通过 tinyUSB 的 IN 端点发送给主机

### 5.5 控制传输转发

1. 主机向 P4 发送控制传输（如 GET_REPORT、SET_IDLE、自定义请求等）
2. tinyUSB 控制传输回调触发，P4 封装为 `CMD_CTRL_REQ` 发给 Linux
3. Linux 收到后，用 libusb 向真实设备发起相同的控制传输
4. Linux 收到真实设备的响应后：
   - 有数据返回 → 发送 `CMD_CTRL_DATA` 携带响应数据
   - 设备返回错误/不支持 → 发送 `CMD_CTRL_STALL`
5. P4 收到响应后，完成对主机的控制传输应答

> **超时处理**：如果 Linux 在 1000ms 内未响应 `CMD_CTRL_REQ`，P4 对主机返回 STALL。

### 5.6 停止与断开

1. Linux 发送 `CMD_STOP_EMULATE`，P4 停止模拟并回复 `CMD_STOP_ACK`
2. P4 断开与主机的 USB 连接
3. P4 清理已注册的描述符，回到空闲状态

---

## 6. 错误处理

### 6.1 CMD_ERROR 格式

```
┌──────────────┬────────────┬─────────────────┐
│ ORIGIN_CMD   │ ERROR_CODE │ ERROR_MSG (opt) │
│   1 byte     │  1 byte    │  0~128 bytes    │
└──────────────┴────────────┴─────────────────┘
```

| 字段 | 说明 |
|------|------|
| ORIGIN_CMD | 触发错误的原始指令 CMD |
| ERROR_CODE | 错误码，见下表 |
| ERROR_MSG | 可选的错误描述字符串（UTF-8，无 null 终止） |

### 6.2 错误码

| 错误码 | 名称 | 说明 |
|--------|------|------|
| `0x01` | ERR_CRC | CRC 校验失败 |
| `0x02` | ERR_FRAME | 帧格式错误（长度不符、非法转义等） |
| `0x03` | ERR_CMD | 未知指令 |
| `0x04` | ERR_STATE | 当前状态不允许执行该指令（如未绑定时发报表） |
| `0x05` | ERR_DESC | 描述符数据错误（超出范围、序号缺失等） |
| `0x06` | ERR_USB | USB 操作失败（P4 tinyUSB 错误） |
| `0x07` | ERR_TIMEOUT | 操作超时 |
| `0x08` | ERR_BUSY | P4 正忙（已在模拟其他设备） |
| `0xFF` | ERR_UNKNOWN | 未知错误 |

### 6.3 超时与重传

| 场景 | 超时时间 | 重试次数 | 超时后动作 |
|------|---------|---------|-----------|
| CMD_PING → CMD_PONG | 500ms | 3 | 判定 P4 离线 |
| CMD_DESC_BUNDLE → CMD_DESC_ACK | 2000ms | 3 | 重新发送当前描述符 |
| CMD_CTRL_REQ → CMD_CTRL_DATA/STALL | 1000ms | 0 | P4 对主机返回 STALL |
| CMD_START_EMULATE → CMD_EMULATE_ACK | 5000ms | 1 | 报告启动失败 |
| 心跳间隔（正常运行时） | 5000ms | — | 超过 3 次无响应判定断连 |

### 6.4 断线检测与恢复

- **心跳机制**：正常运行期间，Linux 每 5 秒发送 `CMD_PING`，P4 回复 `CMD_PONG`
- **断线判定**：连续 3 次心跳无响应（15 秒），判定连接断开
- **恢复流程**：重新执行第 5.2 节的连接建立流程，重新绑定和传输描述符

---

## 7. 多设备管理

### 7.1 拓扑模型

```
┌─────────────────────────────────────────────┐
│            Linux 上位机                      │
│                                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
│  │ USB 设备A│  │ USB 设备B│  │ USB 设备C│  │
│  │ (libusb) │  │ (libusb) │  │ (libusb) │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  │
│       │             │             │         │
│  ┌────▼─────┐  ┌────▼─────┐      │         │
│  │串口/tty1 │  │串口/tty2 │      │         │
│  │  (UART)  │  │  (UART)  │      │         │
│  └────┬─────┘  └────┬─────┘      │         │
└───────┼──────────────┼───────────┼──────────┘
        │              │           │
   ┌────▼─────┐  ┌────▼─────┐  ┌──▼───┐
   │  P4 #1   │  │  P4 #2   │  │P4 #3 │
   │(设备A模拟)│  │(设备B模拟)│  │(空闲) │
   └──────────┘  └──────────┘  └──────┘
```

### 7.2 绑定关系

Linux 端通过配置或 CLI 指定绑定关系：

```
USB 设备 (bus:dev / VID:PID)  ←→  串口设备 (/dev/ttyACMx)  ←→  P4 实例
```

- 每个串口设备代表一个 P4 实例
- 每个 USB 设备代表一个要模拟的目标
- 绑定关系是一对一的：一个 USB 设备绑定一个 P4

### 7.3 Linux 端多实例 (Golang)

Linux 程序内部使用 goroutine 管理每个绑定关系：

```
main goroutine
├── device goroutine 1: USB设备A ↔ /dev/ttyACM0 (P4 #1)
│   ├── serial reader goroutine
│   ├── serial writer goroutine
│   ├── hid report poll goroutine
│   └── ctrl transfer handler goroutine
├── device goroutine 2: USB设备B ↔ /dev/ttyACM1 (P4 #2)
│   └── ...
└── monitor goroutine: 监听 USB 热插拔事件 (hotplug callback)
```

每个设备 goroutine 内部通过 channel 通信：
- `chan Frame` — 串口收发帧
- `chan CtrlReq` — 控制传输请求队列
- `chan []byte` — HID 报表数据流

---

## 8. tinyUSB 集成要点

### 8.1 动态描述符注册

P4 端在 `CMD_DESC_BUNDLE` 接收完成后，需要：

1. 将收到的描述符数据存储到 RAM 中
2. 修改 tinyUSB 的描述符表，使其指向 RAM 中的数据
3. 通过 `tud_descriptor_device_cb()`、`tud_descriptor_configuration_cb()` 等回调返回动态描述符

### 8.2 控制传输拦截

P4 端需要在 tinyUSB 的 `tud_control_xfer_cb()` 中拦截所有控制传输：

1. 对于标准请求（GET_DESCRIPTOR、SET_CONFIGURATION 等），如果描述符已在本地注册，本地响应
2. 对于非标准请求或需要真实设备状态的请求，封装为 `CMD_CTRL_REQ` 转发给 Linux
3. 在收到 Linux 的 `CMD_CTRL_DATA` 或 `CMD_CTRL_STALL` 前，阻塞对主机的响应

### 8.3 HID 报表发送

P4 端收到 `CMD_HID_REPORT` 后：

1. 根据 INTERFACE 字段确定目标接口
2. 通过 `tud_hid_report()` 将报表数据发送给主机
3. 如果对应接口的 IN 端点尚未准备好（主机未轮询），缓存报表数据

---

## 9. 配置参数

### 9.1 协议参数

| 参数 | 值 | 说明 |
|------|-----|------|
| 默认波特率 | 4000000 (4M) | Release 模式 |
| Debug 波特率 | 115200 | 通过 CLI `--debug` 启用 |
| 心跳间隔 | 5000ms | 正常运行期间 PING 间隔 |
| 心跳断连阈值 | 3 次 | 连续 3 次无响应判定断连 |
| PING 超时 | 500ms | 握手 PING→PONG 超时 |
| PING 重试 | 3 次 | 握手重试次数 |
| 描述符 ACK 超时 | 2000ms | DESC_BUNDLE→DESC_ACK |
| 控制传输超时 | 1000ms | CTRL_REQ→CTRL_DATA/STALL |
| 仿真启动超时 | 5000ms | START_EMUL→EMUL_ACK |
| 单帧最大数据量 | 4089 bytes | 描述符分包数据 |

### 9.2 CLI 用法

Linux 端编译：
```bash
cd client
go build -o usb-reader ./cmd/
```

```bash
# 列出可用 USB 设备
./usb-reader list

# 启动仿真 (指定 bus:dev)
./usb-reader start --bus 1 --dev 3

# 启动仿真 (指定串口设备)
./usb-reader start -s /dev/ttyACM0 --bus 1 --dev 3

# Debug 模式 (115200 波特率)
./usb-reader start --bus 1 --dev 3 --debug
```

| 参数 | 说明 |
|------|------|
| `-s, --serial` | 串口设备路径（默认自动检测 /dev/ttyACM*） |
| `--bus` | 目标 USB 设备总线号（通过 list 命令获取） |
| `--dev` | 目标 USB 设备地址（通过 list 命令获取） |
| `--debug` | Debug 模式（115200 波特率） |
