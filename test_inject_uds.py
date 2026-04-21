#!/usr/bin/env python3
"""HID injection via Unix Domain Socket (unixgram).

Usage:
    python3 test_inject_uds.py [/path/to/socket]

    Default socket path: /tmp/hid.socket
    For abstract socket, use: @hid
"""
import socket
import struct
import time
import sys

def send_hid_report_uds(iface, data, sock_path='/tmp/hid.socket'):
    """Send a HID injection packet via UDS (unixgram).

    Packet format:
      [0xC0] [iface] [len_hi] [len_lo] [data...]
    """
    pkt = struct.pack('>BBH', 0xC0, iface, len(data)) + data
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
    sock.sendto(pkt, sock_path)
    sock.close()

def mouse_left_click(iface=0, sock_path='/tmp/hid.socket'):
    """Simulate a mouse left click.

    Based on debug log from real device (13-byte HID report):
      Press:   01 00 00 00 00 00 00 00 01 93 40 00 00
      Release: 00 00 00 00 00 00 00 00 01 93 40 00 00
    Byte 0 bit 0 = left button, rest = position/scroll data.
    """
    # Left button down
    send_hid_report_uds(iface, bytes.fromhex('01000000000000000193400000'), sock_path)
    time.sleep(0.05)
    # Left button up
    send_hid_report_uds(iface, bytes.fromhex('00000000000000000193400000'), sock_path)

def mouse_move(dx=0, dy=0, iface=0, sock_path='/tmp/hid.socket'):
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
    send_hid_report_uds(iface, bytes(baseline), sock_path)

if __name__ == '__main__':
    sock_path = sys.argv[1] if len(sys.argv) > 1 else '/tmp/hid.socket'

    print(f"Sending mouse left click via UDS {sock_path}")
    mouse_left_click(iface=0, sock_path=sock_path)
    print("Done")
