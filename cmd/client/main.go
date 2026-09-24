package main

import (
	"flag"
	"fmt"
	"os"
	"quiclab/mobile"
	"time"
)

type sink struct{}

func (sink) OnEvent(event string) { fmt.Println(event) }

func main() {
	endpoint := flag.String("server", "127.0.0.1:4433", "numeric IP:port")
	name := flag.String("name", "", "TLS server name when using PKI")
	pin := flag.String("pin", "", "SHA-256 certificate fingerprint for lab certificate")
	duration := flag.Duration("duration", 15*time.Second, "experiment duration")
	migrate := flag.Duration("migrate-every", 5*time.Second, "switch to a new UDP socket; zero disables")
	interval := flag.Int("interval-ms", 50, "echo interval")
	flag.Parse()
	c := mobile.NewClient(sink{})
	if err := c.Start(*endpoint, *name, *pin, *interval, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer c.Stop()
	end := time.NewTimer(*duration)
	deadline := time.Now().Add(*duration)
	defer end.Stop()
	var ticks <-chan time.Time
	if *migrate > 0 {
		t := time.NewTicker(*migrate)
		defer t.Stop()
		ticks = t.C
	}
	for {
		if !time.Now().Before(deadline) {
			return
		}
		select {
		case <-end.C:
			return
		case <-ticks:
			if err := c.Migrate(nil); err != nil {
				fmt.Fprintln(os.Stderr, "migration:", err)
				ticks = nil
			}
		}
	}
}
