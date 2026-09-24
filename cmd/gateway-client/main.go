// gateway-client is a desktop SOCKS5 entry point for controlled tunnel experiments.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"quiclab/mobile"
)

func main() {
	mode := flag.String("transport", "quic", "quic or https")
	endpoint := flag.String("server", "", "gateway host:port")
	name := flag.String("name", "", "TLS hostname")
	cert := flag.String("cert", "", "client certificate PEM")
	key := flag.String("key", "", "client key PEM")
	ca := flag.String("ca", "", "optional server CA PEM")
	listen := flag.String("socks", "127.0.0.1:1080", "loopback SOCKS5 CONNECT IPv4 listener")
	flag.Parse()
	read := func(p string) string {
		b, e := os.ReadFile(p)
		if e != nil {
			log.Fatal(e)
		}
		return string(b)
	}
	host, port, e := net.SplitHostPort(*endpoint)
	if e != nil {
		log.Fatal(e)
	}
	ips, e := net.LookupIP(host)
	if e != nil {
		log.Fatal(e)
	}
	address := ""
	for _, ip := range ips {
		if ip.To4() != nil {
			address = net.JoinHostPort(ip.String(), port)
			break
		}
	}
	if address == "" {
		log.Fatal("no IPv4 server address")
	}
	if *name == "" {
		*name = host
	}
	cfg := map[string]string{"transport": *mode, "endpoint": address, "hostname": *name, "certificate": read(*cert), "key": read(*key)}
	if *ca != "" {
		cfg["ca"] = read(*ca)
	}
	b, _ := json.Marshal(cfg)
	g := mobile.NewGateway(nil)
	if e = g.Start(string(b), nil); e != nil {
		log.Fatal(e)
	}
	defer g.Stop()
	bound, e := g.ListenSOCKS(*listen)
	if e != nil {
		log.Fatal(e)
	}
	fmt.Println("SOCKS5 CONNECT IPv4:", bound)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}
