# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

linux-cu is a USB HID passthrough emulation system for Linux. It reads HID data from a real USB device via libusb and creates a virtual USB Gadget device using Linux ConfigFS + `usb_f_hid` kernel module, enabling bidirectional HID report forwarding. Supports cross-machine passthrough via UDP/UDS IPC.

Targets Linux systems with USB Gadget support (Raspberry Pi, USB Armory). Requires root at runtime.

## Build & Test Commands

```bash
# Build (requires libusb-1.0-0-dev, pkg-config)
go build -o linux-cu ./cmd/

# Run all tests
go test ./...

# Vet
go vet ./...

# Run single test
go test ./cmd/ -run TestFFS
go test ./pkg/profile/ -run TestProfile
```

## Architecture

The system uses a **pipeline architecture** with a central unified pipe (`pkg/pipe`) connecting all I/O:

```
Real USB Device (libusb) ←→ Unified Pipe (chan PipeMsg) ←→ Gadget (/dev/hidgN)
                                      ↕
                              IPC (UDS/UDP)
```

**Key packages:**
- `pkg/usb` — CGo libusb wrapper (the ONLY package using CGo). Enumerates devices, reads descriptors, handles interrupt/bulk/control transfers.
- `pkg/gadget` — ConfigFS gadget lifecycle: create gadget dirs, add HID/FFS functions, bind to UDC, cleanup on exit.
- `pkg/pipe` — Central message bus: two buffered channels (`HostToDevice`, `DeviceToHost`) carrying `PipeMsg` structs with direction, type, interface, endpoint, and data fields.
- `pkg/profile` — YAML serialization of full USB device descriptor trees (device → config → interface → endpoint → HID report).
- `cmd/` — CLI application (Cobra). Subcommands: `list`, `save`, `load`, `emulate`, `send`.

**Key cmd/ files:**
- `main.go` — CLI entry, 5 subcommands
- `emulate.go` — Full passthrough: real device → gadget + optional IPC
- `load.go` — Create gadget from YAML (IPC injection only, no real device)
- `send.go` — Read real device, send to network/IPC target (no gadget)
- `gadgetio.go` — Opens `/dev/hidgN`, bridges pipe ↔ gadget device, proxies control transfers
- `hidpoll.go` — Polls real USB device IN endpoints, forwards OUT data back
- `ipc.go` — UDS/UDP listeners/writers, custom binary packet format (magic `0xC0`)
- `ffsdesc.go` / `ffsio.go` — FunctionFS support for non-HID interfaces (e.g., Xbox audio)

## IPC Packet Format

Binary format for external HID injection via `--uds` / `--udp`:
```
Offset 0: Magic (0xC0)
Offset 1: Interface Number (uint8)
Offset 2-3: Data Length (uint16, big-endian)
Offset 4+: HID Report Data
```

## Dependencies

**Go:** `cobra` (CLI), `yaml.v3` (profiles)
**System:** `libusb-1.0` (via CGo), Linux kernel modules: `libcomposite`, `usb_f_hid`, ConfigFS at `/sys/kernel/config/`

## Profile YAML Files

Saved USB device profiles (*.yaml in root) contain full descriptor trees used by `load` command to recreate gadgets without a real device. Use `save` to create them from physical devices.

## Notes

- Gadget lifecycle is fully managed: create on start, destroy on exit (via `defer` + signal handlers)
- FFS (FunctionFS) handles non-HID interfaces separately from `usb_f_hid` functions
- The `PipeMsg` struct is the universal data unit — all USB data and control transfers flow through it
- `send` command target format: `PROTO:address` (e.g., `UDP:192.168.3.3:9981`, `UDS:@hid`)
- CGo is isolated to `pkg/usb/` only; all other packages are pure Go
