# linux-cu

USB HID 透传模拟系统。基于 Linux USB Gadget FunctionFS，在本机直接模拟 USB 设备，实现数据透传。

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                      linux-cu                               │
│                                                             │
│  ┌──────────┐    ┌──────────────┐    ┌──────────────────┐   │
│  │ USB 设备  │    │  统一管道     │    │  Gadget 设备     │   │
│  │ (libusb)  │───▶│  (Pipe)      │───▶│  (FunctionFS)   │   │
│  │ 读取数据  │◀───│  双向事件/数据 │◀───│  模拟设备       │   │
│  └──────────┘    └──────────────┘    └──────────────────┘   │
│                                                             │
│  流程: list → 选择设备 → 创建 gadget → 透传数据 → 退出销毁    │
└─────────────────────────────────────────────────────────────┘
```

核心设计：
- **FunctionFS**：通过 Linux ConfigFS + FunctionFS 创建/销毁 Gadget USB 设备
- **统一管道**：所有事件与数据通过一对 `chan PipeMsg` 双向传输，便于扩展
- **透传**：从真实 USB 设备读取 HID 数据，写入 Gadget 端点，反之亦然

## 依赖

```bash
# Ubuntu / Debian
sudo apt install libusb-1.0-0-dev pkg-config golang linux-headers-$(uname -r)

# 需要内核支持: ConfigFS, FunctionFS, USB Gadget
# 检查:
ls /sys/kernel/config/usb_gadget  # ConfigFS
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
sudo ./linux-cu emulate -b 2:32

# 模拟指定设备 (VID:PID)
sudo ./linux-cu emulate -d 046d:c08b
```

> 需要 root 权限操作 ConfigFS 和 FunctionFS。程序退出时自动销毁 Gadget 设备。

## 项目结构

```
cmd/
└── main.go              # CLI 入口，list / emulate 子命令
pkg/
├── gadget/
│   ├── gadget.go         # ConfigFS + FunctionFS gadget 创建/销毁
│   └── ffs.go            # FunctionFS 描述符写入与端点 IO
├── pipe/
│   └── pipe.go           # 统一双向管道，事件与数据传输
└── usb/
    ├── descriptor.go     # cgo libusb 绑定，描述符读取
    ├── device.go         # USB 设备句柄管理
    ├── transfer.go       # 控制传输代理
    └── print.go          # 格式化终端输出
```

## 工作流程

1. **list**：枚举系统 USB 设备，显示 VID:PID 和设备名称
2. **emulate**：
   - 读取目标设备的完整描述符（设备/配置/接口/端点/HID报告）
   - 检查是否已存在同 VID:PID 的 Gadget 设备，存在则先销毁
   - 通过 ConfigFS 创建 Gadget 设备，配置描述符
   - 通过 FunctionFS 注册 HID 功能，写入描述符
   - 连接 Gadget 到 UDC (USB Device Controller)
   - 建立统一管道，启动双向数据透传：
     - 真实设备 IN 端点 → 管道 → Gadget IN 端点
     - Gadget OUT 端点 → 管道 → 真实设备 OUT 端点
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
