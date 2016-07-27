package bonjour

import (
	"fmt"
	"log"
	"net"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var (
	// Multicast groups used by mDNS
	mdnsGroupIPv4 = net.IPv4(224, 0, 0, 251)
	mdnsGroupIPv6 = net.ParseIP("ff02::fb")

	// mDNS wildcard addresses
	mdnsWildcardAddrIPv4 = &net.UDPAddr{
		IP:   net.ParseIP("224.0.0.0"),
		Port: 5353,
	}
	mdnsWildcardAddrIPv6 = &net.UDPAddr{
		IP: net.ParseIP("ff02::"),
		// IP:   net.ParseIP("fd00::12d3:26e7:48db:e7d"),
		Port: 5353,
	}

	// mDNS endpoint addresses
	ipv4Addr = &net.UDPAddr{
		IP:   mdnsGroupIPv4,
		Port: 5353,
	}
	ipv6Addr = &net.UDPAddr{
		IP:   mdnsGroupIPv6,
		Port: 5353,
	}
)

func newConnection(iface *net.Interface) (*net.UDPConn, *net.UDPConn, error) {
	// Create wildcard connections (because :5353 can be already taken by other apps)
	// This is also useful as it allows to run a server and client instance simultaneously
	ipv4conn, err := net.ListenUDP("udp4", mdnsWildcardAddrIPv4)
	if err != nil {
		log.Printf("[ERR] bonjour: Failed to bind to udp4 port: %v", err)
	}
	ipv6conn, err := net.ListenUDP("udp6", mdnsWildcardAddrIPv6)
	if err != nil {
		log.Printf("[ERR] bonjour: Failed to bind to udp6 port: %v", err)
	}
	if ipv4conn == nil && ipv6conn == nil {
		return nil, nil, fmt.Errorf("[ERR] bonjour: Failed to bind to any udp port!")
	}

	// Join multicast groups to receive announcements
	p1 := ipv4.NewPacketConn(ipv4conn)
	p2 := ipv6.NewPacketConn(ipv6conn)
	if iface != nil {
		if err := p1.JoinGroup(iface, &net.UDPAddr{IP: mdnsGroupIPv4}); err != nil {
			return nil, nil, err
		}
		if err := p2.JoinGroup(iface, &net.UDPAddr{IP: mdnsGroupIPv6}); err != nil {
			return nil, nil, err
		}
	} else {
		ifaces, err := net.Interfaces()
		log.Println(">> All interfaces: ", ifaces)
		if err != nil {
			return nil, nil, err
		}
		errCount1, errCount2 := 0, 0
		for _, iface := range ifaces {
			if iface.Index == 23 {
				log.Println("bind to ipv6 iface ", iface)

				if err := p1.JoinGroup(&iface, &net.UDPAddr{IP: mdnsGroupIPv4}); err != nil {
					log.Println("ipv4 joingroup failed for iface ", iface)
					errCount1++
				}
				if err := p2.JoinGroup(&iface, &net.UDPAddr{IP: mdnsGroupIPv6}); err != nil {
					log.Println("ipv6 joingroup failed for iface ", iface)
					errCount2++
				}
			}

		}
		if len(ifaces) == errCount1 && len(ifaces) == errCount2 {
			return nil, nil, fmt.Errorf("Failed to join multicast group on all interfaces!")
		}
	}

	return ipv4conn, ipv6conn, nil
}
