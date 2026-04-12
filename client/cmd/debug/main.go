package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/linux-cu/client/pkg/serial"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: debug /dev/ttyACM0\n")
		os.Exit(1)
	}
	device := os.Args[1]

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	log.Printf("Opening %s...", device)
	port, err := serial.Open(ctx, serial.Config{Device: device, Debug: true})
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer port.Close()

	log.Println("Port open. Sending PING frame (0x01)...")
	port.SendFrame(serial.Frame{Cmd: 0x01})

	log.Println("Waiting for responses (5s)...")
	_ = ctx
	for {
		select {
		case f := <-port.RecvChan():
			log.Printf("RECV: cmd=0x%02X payload=%v", f.Cmd, f.Payload)
		case raw := <-port.RawChan():
			log.Printf("RAW: %s", string(raw))
		case err := <-port.ErrChan():
			log.Fatalf("ERROR: %v", err)
		}
	}
}
