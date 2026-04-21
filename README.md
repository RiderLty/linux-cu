# linux-cu

USB HID 透传模拟系统。基于 Linux USB Gadget ConfigFS + usb_f_hid，在本机直接模拟 USB 设备，实现数据透传。

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                      linux-cu                               │
│                                                             │
│  ┌──────────┐    ┌──────────────┐    ┌──────────────────┐   │
│  │ USB 设备  │    │  统一管道     │    │  Gadget 设备     │   │
│  │ (libusb)  │───▶│  (Pipe)      │───▶│  (usb_f_hid)    │   │
│  │ 读取数据  │◀───│  双向事件/数据 │◀───│  模拟设备       │   │
│  └──────────┘    └──────────────┘    └──────────────────┘   │
│                                                             │
│  流程: list → 选择设备 → 创建 gadget → 透传数据 → 退出销毁    │
└─────────────────────────────────────────────────────────────┘
```

核心设计：
- **usb_f_hid**：通过 Linux ConfigFS + usb_f_hid 内核模块创建 Gadget HID 设备，自动生成 /dev/hidgN 设备节点
- **统一管道**：所有事件与数据通过一对 `chan PipeMsg` 双向传输，便于扩展
- **透传**：从真实 USB 设备读取 HID 数据，写入 /dev/hidgN，反之亦然

## 依赖

```bash
# Ubuntu / Debian
sudo apt install libusb-1.0-0-dev pkg-config golang linux-headers-$(uname -r)

# 需要内核支持: ConfigFS, usb_f_hid, USB Gadget
# 检查:
ls /sys/kernel/config/usb_gadget  # ConfigFS
ls /sys/module/usb_f_hid          # usb_f_hid 模块
```

## 构建

```bash
go mod tidy
go build -o linux-cu ./cmd/
```

## 使用

```bash
# 列出所有 USB 设备
sudo ./linux-cu list

# 模拟指定设备 (bus:dev)
sudo ./linux-cu emulate --bus 2 --dev 32

# 模拟指定设备 (VID:PID)
sudo ./linux-cu emulate --vid 046d --pid c08b

# 启用调试模式，显示所有交互数据
sudo ./linux-cu emulate --bus 2 --dev 32 --debug

# 启用 UDS 事件注入 (文件套接字)
sudo ./linux-cu emulate --bus 2 --dev 32 --uds /tmp/hid.sock

# 启用 UDS 事件注入 (抽象套接字，@ 前缀)
sudo ./linux-cu emulate --bus 2 --dev 32 --uds @hid

# 启用 UDP 事件注入 (监听所有 IP 的 9090 端口)
sudo ./linux-cu emulate --bus 2 --dev 32 --udp :9090

# 启用 UDP 事件注入 (仅监听 127.0.0.1:9090)
sudo ./linux-cu emulate --bus 2 --dev 32 --udp 127.0.0.1:9090

# 同时启用 UDS + UDP + 调试
sudo ./linux-cu emulate --bus 2 --dev 32 --uds @hid --udp :9090 --debug

# 保存设备信息到 YAML 文件
sudo ./linux-cu save --bus 2 --dev 32
sudo ./linux-cu save --vid 046d --pid c08b -o my_mouse.yaml

# 从 YAML 文件创建 Gadget 设备 (仅 IPC 注入，无真实设备透传)
sudo ./linux-cu load my_mouse.yaml --udp :9999 --debug
sudo ./linux-cu load 046d_c08b.yaml --uds @hid
```

> 需要 root 权限操作 ConfigFS 和 /dev/hidgN 设备。程序退出时自动销毁 Gadget 设备。

## 项目结构

```
cmd/
├── main.go              # CLI 入口，list / emulate / save / load 子命令
├── emulate.go           # emulate 主逻辑
├── save.go              # save 保存设备信息
├── load.go              # load 从文件创建 Gadget
├── gadgetio.go          # Gadget /dev/hidgN 读写
├── hidpoll.go           # 真实设备 HID 轮询与 OUT 转发
└── ipc.go               # UDS/UDP 事件注入
pkg/
├── gadget/
│   ├── gadget.go         # ConfigFS gadget 创建/销毁
│   └── hid.go            # usb_f_hid HID 功能创建与 /dev/hidgN IO
├── pipe/
│   └── pipe.go           # 统一双向管道，事件与数据传输
├── profile/
│   └── profile.go       # 设备信息 YAML 序列化/反序列化
└── usb/
    ├── descriptor.go     # cgo libusb 绑定，描述符读取
    ├── device.go         # USB 设备句柄管理
    ├── transfer.go       # 控制传输代理
    └── print.go          # 格式化终端输出
```

## 工作流程

1. **list**：枚举系统 USB 设备，显示 VID:PID 和设备名称
2. **save**：读取指定 USB 设备的完整描述符，保存为 YAML 文件
3. **load**：从 YAML 文件创建 Gadget 设备（无真实设备透传，仅支持 IPC 注入）
4. **emulate**：
   - 读取目标设备的完整描述符（设备/配置/接口/端点/HID报告）
   - 检查是否已存在同 VID:PID 的 Gadget 设备，存在则先销毁
   - 通过 ConfigFS 创建 Gadget 设备，配置描述符
   - 为每个 HID 接口创建 usb_f_hid 功能，写入报告描述符
   - 连接 Gadget 到 UDC (USB Device Controller)
   - 建立统一管道，启动双向数据透传：
     - 真实设备 IN 端点 → 管道 → /dev/hidgN 写入
     - /dev/hidgN 读取 → 管道 → 真实设备 OUT 端点
     - Gadget 控制传输请求 → 管道 → 真实设备控制传输
   - 退出时销毁 Gadget 设备

## 统一管道设计

```go
type PipeMsg struct {
    Direction  Direction  // HostToDevice 或 DeviceToHost
    Type       MsgType    // Data / Control / Event
    Endpoint   uint8      // 端点地址
    Interface  uint8      // 接口号
    Data       []byte     // 数据负载
}
```

所有 USB 数据和控制传输都封装为 `PipeMsg`，通过一对 channel 传输：
- `hostToDevice chan PipeMsg`：主机→设备方向
- `deviceToHost chan PipeMsg`：设备→主机方向

这种设计便于：
- 添加日志/过滤/转换中间层
- 支持多设备同时透传
- 远程透传（未来可通过网络传输 PipeMsg）

## USB HID 鼠标回报率问题

Linux 内核的 `usbhid` 驱动默认使用设备描述符中的 `bInterval` 作为轮询间隔，这可能导致鼠标回报率被限制（例如 62Hz）。

### 原因

`usbhid` 模块的 `mousepoll` 参数控制鼠标轮询间隔：
- 默认值 `0` 或 `UINT_MAX`：使用设备自身的 `bInterval`，部分鼠标该值较大，导致低回报率
- 手动设置可强制指定轮询间隔（单位：毫秒）

### 解决方案

如果 `usbhid` 是可加载模块：

```bash
# 设置 1ms 轮询间隔 (1000Hz)
sudo modprobe -r usbhid && sudo modprobe usbhid mousepoll=1
```

如果 `usbhid` 是内建模块（树莓派等），需通过内核启动参数设置：

```bash
# 编辑 /boot/firmware/cmdline.txt，在行尾添加：
usbhid.mousepoll=1

# 然后重启
sudo reboot
```

### mousepoll 参数说明

| 值 | 轮询间隔 | 回报率 |
|---|---|---|
| 0 | 使用设备默认 bInterval | 取决于设备 |
| 1 | 1ms | 1000Hz |
| 2 | 2ms | 500Hz |
| 4 | 4ms | 250Hz |
| 8 | 8ms | 125Hz |

### 使用设备默认回报率

如果想恢复使用设备自身的默认回报率，将 `mousepoll` 设为 `0`：

```bash
# 内建模块方式：编辑 /boot/firmware/cmdline.txt，设置：
usbhid.mousepoll=0

# 可加载模块方式：
sudo modprobe -r usbhid && sudo modprobe usbhid mousepoll=0
```

### 验证

```bash
# 查看当前 mousepoll 值
cat /sys/module/usbhid/parameters/mousepoll

# 使用 evtest 监测实际回报率
sudo apt install evtest
sudo evtest /dev/input/eventX
```

## 外部事件注入 (IPC)

通过 `--uds` 或 `--udp` 参数，可以开启外部事件注入功能，允许外部程序通过网络或 Unix 域套接字向 Gadget 设备注入 HID 数据。

### 参数说明

| 参数 | 格式 | 说明 |
|---|---|---|
| `--uds` | `/path/to/socket` | Unix 域套接字文件路径 |
| `--uds` | `@name` | 抽象套接字（`@` 前缀，无需文件） |
| `--udp` | `:port` | 监听所有 IP 的指定端口 |
| `--udp` | `ip:port` | 仅监听指定 IP 的指定端口 |

### 数据包格式

所有注入数据使用以下二进制格式：

```
Offset  Size  Field            说明
0       1     Magic            固定值 0xC0
1       1     Interface Number 目标 USB 接口号 (如 0, 1, 3)
2       2     Data Length      数据长度 N (大端序)
4       N     HID Report Data  HID 报告数据
```

**示例**：向接口 0 注入 4 字节键盘数据 (按键 A)

```
C0 00 00 04 00 00 04 00
│  │  │     └───────── HID 报告数据 (4 bytes)
│  │  └─────────────── 数据长度 = 4 (big-endian)
│  └────────────────── 接口号 = 0
└───────────────────── Magic = 0xC0
```

### 发送示例

**Python (UDP)**:

```python
import socket
import struct

def send_hid_report(iface, data, host='127.0.0.1', port=9090):
    pkt = struct.pack('>B BH', 0xC0, iface, len(data)) + data
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.sendto(pkt, (host, port))

# 向接口 0 注入键盘按键 A
send_hid_report(0, bytes([0x00, 0x00, 0x04, 0x00]))
```

**Python (UDS)**:

```python
import socket
import struct

def send_hid_report(iface, data, addr='/tmp/hid.sock'):
    pkt = struct.pack('>B BH', 0xC0, iface, len(data)) + data
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
    sock.sendto(pkt, addr)

# 使用抽象套接字
# send_hid_report(0, bytes([0x00, 0x00, 0x04, 0x00]), '\0hid')
```

**Shell (UDP)**:

```bash
# 发送原始二进制数据包
echo -ne '\xc0\x00\x00\x04\x00\x00\x04\x00' | nc -u -q0 127.0.0.1 9090
```
