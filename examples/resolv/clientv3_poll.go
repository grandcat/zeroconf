package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

var (
	service  = flag.String("service", "_workstation._tcp", "Set the service category to look for devices.")
	domain   = flag.String("domain", "local", "Set the search domain. For local networks, default is fine.")
	waitTime = flag.Int("wait", 10, "Duration in [s] to run discovery.")
	intf     = flag.String("intf", "", "List of interfaces to register separated by ,")
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	flag.Parse()

	var intfList []net.Interface
	var err error

	if *intf != "" {
		ifaces := strings.Split(*intf, ",")
		for _, iface := range ifaces {
			i, err := net.InterfaceByName(iface)
			if err == nil {
				intfList = append(intfList, *i)
			}
		}
	}

	// Discover all services on the network (e.g. _workstation._tcp)

	// a simpler call would be like below where we listen on all interfaces
	// resolver, err := zeroconf.NewResolver(nil)
	// However if we want to listen on selected interfaces only, use the call below
	resolver, err := zeroconf.NewResolver(zeroconf.SelectIfaces(intfList))
	if err != nil {
		log.Fatalln("Failed to initialize resolver:", err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(*waitTime))
	var b *zeroconf.Browser

	defer cancel()
	b, err = resolver.Browsev3(ctx, *service, *domain, nil)
	if err != nil {
		log.Fatalln("Failed to browse:", err.Error())
	}

	<-ctx.Done()
	fmt.Printf("cached entries: %+v\n", b.Entries())
	// Wait some additional time to see debug messages on go routine shutdown.
	time.Sleep(1 * time.Second)
}
