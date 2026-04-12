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
    time.sleep(0.5) # 等待 ESP32 启动
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
        cmd = input("pico>")
        ser.write((cmd+"\n").encode())



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



