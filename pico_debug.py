import serial
import time
import threading
import socket

COM_PORT = '/dev/ttyACM0'
BAUDRATE = 4000000
if __name__ == "__main__":
    ser = serial.Serial(COM_PORT,BAUDRATE )  
    ser.setDTR(False) 
    ser.setRTS(True)
    time.sleep(0.1)
    ser.setDTR(True)
    ser.setRTS(False)
    def reader():
        while True:
            data = ser.readline()
            try:
                print(data.decode().strip())
            except:
                print(data)
    thread = threading.Thread(target=reader)
    thread.start()
    while True:
        # PING 帧: [0x55][0xAA][len_lo][len_hi][cmd]
        # CMD_PING = 0x01, len = 1 (cmd only, no payload)
        ser.write(b'\x55\xAA\x01\x00\x01')
        time.sleep(1)
        print("PING")


    # if TARGET == SERIAL:
    #     time.sleep(1)
    #     i = 0
    #     while i < 0x7ffffffe:  
    #         touch_down(0,i,i)
    #         i += 2000000
    #         time.sleep(1/RATE)
    #     touch_up(0)
    #     print("ALL DONE!!!!!!!")
    #     time.sleep(1)
    #     # set_wifi("0x386F","1729suj109noisa09iqw3jer")

    # elif TARGET == UDP:
    #     i = 0
    #     while i < 0x7ffffffe:
    #         touch_down(0,i,i)
    #         i += 2000000
    #         time.sleep(1/RATE)
    #     touch_up(0)



