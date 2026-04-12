# linux-cu

USB HID 透传模拟系统。Linux 上位机通过 libusb 读取 USB 设备信息，经串口透传给 ESP32-P4 复现设备行为。

## 依赖

### Linux 上位机 (client)

```bash
# Ubuntu / Debian
sudo apt install libusb-1.0-0-dev pkg-config golang

# Fedora / RHEL
sudo dnf install libusb1-devel pkgconfig golang

# Arch
sudo pacman -S libusb pkg-config go
```

验证：
```bash
pkg-config --cflags -- libusb-1.0
```

### ESP32-P4 (server)

- ESP-IDF v5.x
- tinyusb 组件

## 构建

```bash
cd client
go mod tidy
go build -o usb-reader ./cmd/
```

## 使用

```bash
# 列出所有 USB 设备
./usb-reader list

# 读取指定设备的完整描述符 (bus:dev)
./usb-reader info -b 1:3

# 读取指定设备的完整描述符 (VID:PID)
./usb-reader info -d 046d:c08b
```

## 项目结构

```
client/          # Linux 上位机 (Golang + cgo libusb)
├── cmd/
│   └── main.go  # CLI 入口，list / info 子命令
└── pkg/
    └── usb/
        ├── descriptor.go  # cgo libusb 绑定，描述符读取
        └── print.go       # 格式化终端输出
server/          # ESP32-P4 固件 (ESP-IDF C + tinyUSB)
common/          # 共享协议定义
```

## 协议

详见 [protocol.MD](protocol.MD)。
