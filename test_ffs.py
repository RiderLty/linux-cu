#!/usr/bin/env python3
import struct, sys, os as os_mod

# Interface: vendor-specific, 1 endpoint
iface = bytes([9, 4, 0, 0, 1, 0xFF, 0, 0, 0])
# Endpoint: IN bulk
ep = bytes([7, 5, 0x81, 2, 64, 0, 0])
descs = iface + ep

# Try v1 format: magic=1, length, fs_count, hs_count
header = struct.pack("<IIII", 1, 16 + len(descs) * 2, 1, 1)
data = header + descs + descs

fd = os_mod.open("/dev/usb-ffs/ffs.usb0/ep0", os_mod.O_RDWR)
os_mod.write(fd, data)
os_mod.close(fd)
print(f"wrote {len(data)} bytes (v1 format)")
