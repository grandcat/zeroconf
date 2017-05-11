package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"time"

	"github.com/grandcat/zeroconf"
)

var (
	name     = flag.String("name", "GoZeroconfGo", "The name for the service.")
	service  = flag.String("service", "_workstation._tcp", "Set the service type of the new service.")
	domain   = flag.String("domain", "local.", "Set the network domain. Default should be fine.")
	port     = flag.Int("port", 42424, "Set the port the service is listening to.")
	waitTime = flag.Int("wait", 10, "Duration in [s] to publish service for.")
	intf     = flag.String("intf", "", "List of interfaces to register separated by ,")
)

func main() {
	flag.Parse()

	zeroconf.DefaultTTL = 5
	var intfList []net.Interface

	if *intf != "" {
		ifaces := strings.Split(*intf, ",")
		for _, iface := range ifaces {
			i, err := net.InterfaceByName(iface)
			if err == nil {
				intfList = append(intfList, *i)
			}
		}
	}
	server, err := zeroconf.Register(*name, *service, *domain, *port, []string{"txtv=0", "lo=1", "la=2"}, intfList)
	if err != nil {
		panic(err)
	}
	defer server.Shutdown()
	log.Println("Published service:")
	log.Println("- Name:", *name)
	log.Println("- Type:", *service)
	log.Println("- Domain:", *domain)
	log.Println("- Port:", *port)

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
