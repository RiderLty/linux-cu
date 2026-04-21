#!/usr/bin/env python3
import socket
import struct
import time
import sys

'''
mouse left button click
2026/04/21 22:27:34 [DEBUG][HID→Pipe] iface=0 ep=0x81 len=13 data=01000000000000000193400000
2026/04/21 22:27:34 [DEBUG][Pipe→HIDG] iface=0 hidg0 len=13 data=01000000000000000193400000
2026/04/21 22:27:34 [DEBUG][HID→Pipe] iface=0 ep=0x81 len=13 data=00000000000000000193400000
2026/04/21 22:27:34 [DEBUG][Pipe→HIDG] iface=0 hidg0 len=13 data=00000000000000000193400000
'''


def send_hid_report(iface, data, host='127.0.0.1', port=9999):
    """Send a HID injection packet via UDP.

    Packet format:
      [0xC0] [iface] [len_hi] [len_lo] [data...]
    """
    pkt = struct.pack('>BBH', 0xC0, iface, len(data)) + data
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.sendto(pkt, (host, port))
    sock.close()

def mouse_left_click(iface=0, host='127.0.0.1', port=9999):
    """Simulate a mouse left click.

    Based on debug log from real device (13-byte HID report):
      Press:   01 00 00 00 00 00 00 00 01 93 40 00 00
      Release: 00 00 00 00 00 00 00 00 01 93 40 00 00
    Byte 0 bit 0 = left button, rest = position/scroll data.
    """
    # Left button down
    send_hid_report(iface, bytes.fromhex('01000000000000000193400000'), host, port)
    time.sleep(0.05)
    # Left button up
    send_hid_report(iface, bytes.fromhex('00000000000000000193400000'), host, port)

def mouse_move(dx=0, dy=0, iface=0, host='127.0.0.1', port=9999):
    """Simulate mouse movement.

    HID report (13 bytes):
      Byte 0: Buttons (0x00 = none)
      Byte 1-2: X (int16 LE, signed)
      Byte 3-4: Y (int16 LE, signed)
      Rest: unchanged from baseline
    """
    baseline = bytearray.fromhex('00000000000000000193400000')
    struct.pack_into('<h', baseline, 1, dx)
    struct.pack_into('<h', baseline, 3, dy)
    send_hid_report(iface, bytes(baseline), host, port)

if __name__ == '__main__':
    host = sys.argv[1] if len(sys.argv) > 1 else '127.0.0.1'
    port = int(sys.argv[2]) if len(sys.argv) > 2 else 9999

    print(f"Sending mouse left click to {host}:{port}")
    mouse_left_click(iface=0, host=host, port=port)
    print("Done")