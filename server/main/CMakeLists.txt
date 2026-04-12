idf_component_register(
    SRCS "main.c" "tusb_helper.c" "frame.c" "serial.c" "led_helper.c" "nvs_helper.c"
    INCLUDE_DIRS "."
    PRIV_REQUIRES driver nvs_flash esp_driver_gpio esp_driver_usb_serial_jtag
)
