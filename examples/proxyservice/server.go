package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"time"

	"github.com/grandcat/zeroconf"
)

var (
	name     = flag.String("name", "GoZeroconfGo", "The name for the service.")
	service  = flag.String("service", "_workstation._tcp", "Set the service type of the new service.")
	domain   = flag.String("domain", "local.", "Set the network domain. Default should be fine.")
	host     = flag.String("host", "pc1", "Set host name for service.")
	ip       = flag.String("ip", "::1", "Set IP a service should be reachable.")
	port     = flag.Int("port", 42424, "Set the port the service is listening to.")
	waitTime = flag.Int("wait", 10, "Duration in [s] to publish service for.")
)

func main() {
	flag.Parse()

	ifaces, _ := net.Interfaces()

	config := zeroconf.ProxyRegistrationConfig{
		Instance: "GoZeroconfGo",
		Service:  "_workstation._tcp",
		Domain:   "local.",
		Port:     42424,
		Host:     "pc1",
		IPs:      []string{"txtv=0", "lo=1", "la=2"},
		Text:     []string{"key=value", "env=dev"},
		Ifaces:   []net.Interface{ifaces[0]}, // use the first available interface
	}

	server, err := zeroconf.RegisterProxy(config)
	if err != nil {
		panic(err)
	}
	defer server.Shutdown()
	log.Println("Published proxy service:")
	log.Println("- Name:", *name)
	log.Println("- Type:", *service)
	log.Println("- Domain:", *domain)
	log.Println("- Port:", *port)
	log.Println("- Host:", *host)
	log.Println("- IP:", *ip)

	// Clean exit.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	// Timeout timer.
	var tc <-chan time.Time
	if *waitTime > 0 {
		tc = time.After(time.Second * time.Duration(*waitTime))
	}

	select {
	case <-sig:
		// Exit by user
	case <-tc:
		// Exit by timeout
	}

	log.Println("Shutting down.")
}
