---
name: esp32-build
description: This skill should be used when the user asks to "build esp32", "compile firmware", "flash esp32", "idf build", "idf flash", "check build errors", "sync code to windows", or any task involving compiling, flashing, or debugging the ESP32-P4 server firmware. Also applies when discussing sdkconfig, CMakeLists, ESP-IDF errors, or build logs.
version: 1.0.0
---

# ESP32-P4 Build & Flash via Windows SSH

Manage the ESP32-P4 firmware build/flash cycle. Source code lives on this Pi5, builds on a remote Windows machine via SSH.

## Environment

- **Source**: `/home/lty/projects/linux-cu/server/main` (this machine, Pi5)
- **Build host**: `ssh lty@192.168.3.128` (Windows)
- **Remote project**: `E:\Documents\Projects\linux-cu`
- **ESP-IDF**: `C:\Espressif_5.5.3` (ESP-IDF 5.5.3)

## Workflow

### 1. Sync code to Windows

Before any build, sync changed files:

```bash
rclone sync pi5:projects/linux-cu/server/main E:\Documents\Projects\linux-cu\server\main -P
```

This runs on the Windows machine. Execute via SSH:

```bash
ssh lty@192.168.3.128 'rclone sync pi5:projects/linux-cu/server/main E:\Documents\Projects\linux-cu\server\main -P'
```

### 2. Initialize ESP-IDF environment

ESP-IDF must be initialized in each PowerShell session before building. Use this command:

```bash
ssh lty@192.168.3.128 'powershell.exe -ExecutionPolicy Bypass -Command "& C:\WINDOWS\System32\WindowsPowerShell\v1.0\powershell.exe -ExecutionPolicy Bypass -NoExit -File \"C:\Espressif_5.5.3/Initialize-Idf.ps1\" -IdfId esp-idf-3f6d693ecd342813fb78c8a9b3578926"'
```

For build/flash/monitor commands, chain the ESP-IDF init with the idf.py command in a single PowerShell invocation:

```bash
ssh lty@192.168.3.128 'powershell.exe -ExecutionPolicy Bypass -Command "& C:\Espressif_5.5.3\Initialize-Idf.ps1 -IdfId esp-idf-3f6d693ecd342813fb78c8a9b3578926; cd E:\Documents\Projects\linux-cu\server; idf.py build"'
```

### 3. Build

```bash
ssh lty@192.168.3.128 'powershell.exe -ExecutionPolicy Bypass -Command "& C:\Espressif_5.5.3\Initialize-Idf.ps1 -IdfId esp-idf-3f6d693ecd342813fb78c8a9b3578926; cd E:\Documents\Projects\linux-cu\server; idf.py build"'
```

### 4. Flash

```bash
ssh lty@192.168.3.128 'powershell.exe -ExecutionPolicy Bypass -Command "& C:\Espressif_5.5.3\Initialize-Idf.ps1 -IdfId esp-idf-3f6d693ecd342813fb78c8a9b3578926; cd E:\Documents\Projects\linux-cu\server; idf.py flash -p COM3"'
```

Replace `COM3` with the actual COM port. To find it: `ssh lty@192.168.3.128 'mode'` or check Device Manager.

### 5. Monitor

```bash
ssh lty@192.168.3.128 'powershell.exe -ExecutionPolicy Bypass -Command "& C:\Espressif_5.5.3\Initialize-Idf.ps1 -IdfId esp-idf-3f6d693ecd342813fb78c8a9b3578926; cd E:\Documents\Projects\linux-cu\server; idf.py monitor -p COM3"'
```

Note: `idf.py monitor` is interactive and may not work well over SSH. Use it briefly or redirect to a log file.

### 6. Full rebuild (clean + build + flash)

```bash
ssh lty@192.168.3.128 'powershell.exe -ExecutionPolicy Bypass -Command "& C:\Espressif_5.5.3\Initialize-Idf.ps1 -IdfId esp-idf-3f6d693ecd342813fb78c8a9b3578926; cd E:\Documents\Projects\linux-cu\server; idf.py fullclean; idf.py build"'
```

## Debugging Build Errors

1. Sync code first (step 1)
2. Run build (step 3), capture full output
3. Read the error messages, fix code locally on Pi5
4. Repeat from step 1

Common ESP-IDF error patterns:
- **implicit declaration**: Missing `#include` for the function
- **multiple definition**: Symbol defined in both our code and a library — remove our duplicate
- **undefined reference**: Missing source file in `CMakeLists.txt` SRCS, or missing component in `PRIV_REQUIRES`
- **fatal error: xxx.h: No such file**: Missing include path or component dependency

## Important Notes

- Always edit code on Pi5 (this machine), never directly on Windows
- The `rclone sync` is one-way: Pi5 → Windows
- The `server/` directory (not just `server/main/`) is the ESP-IDF project root containing `CMakeLists.txt`, `sdkconfig`, `partitions.csv`
- `sdkconfig` is auto-generated from `sdkconfig.defaults` on first build — do not sync it back
- The `idf.py` commands run in `E:\Documents\Projects\linux-cu\server` (the ESP-IDF project root), not `server/main`
