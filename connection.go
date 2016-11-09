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

func joinUdp6Multicast(interfaces []net.Interface) (*net.UDPConn, error) {
	udpConn, err := net.ListenUDP("udp6", mdnsWildcardAddrIPv6)
	if err != nil {
		log.Printf("[ERR] bonjour: Failed to bind to udp6 mutlicast: %v", err)
		return nil, err
	}

	// Join multicast groups to receive announcements
	pkConn := ipv6.NewPacketConn(udpConn)

	if len(interfaces) == 0 {
		ifaces, err := net.Interfaces()
		if err != nil {
			return nil, err
		}
		interfaces = append(interfaces, ifaces...)
		log.Println("Used interfaces: ", interfaces)
	}

	var failedJoins int
	for _, iface := range interfaces {
		if err := pkConn.JoinGroup(&iface, &net.UDPAddr{IP: mdnsGroupIPv6}); err != nil {
			log.Println("Udp6 JoinGroup failed for iface ", iface)
			failedJoins++
		}
	}
	if failedJoins == len(interfaces) {
		return nil, fmt.Errorf("udp6: failed to join any of these interfaces: %v", interfaces)
	}

	return udpConn, nil
}

func joinUdp4Multicast(interfaces []net.Interface) (*net.UDPConn, error) {
	udpConn, err := net.ListenUDP("udp4", mdnsWildcardAddrIPv4)
	if err != nil {
		log.Printf("[ERR] bonjour: Failed to bind to udp4 mutlicast: %v", err)
		return nil, err
	}

	// Join multicast groups to receive announcements
	pkConn := ipv4.NewPacketConn(udpConn)

	if len(interfaces) == 0 {
		ifaces, err := net.Interfaces()
		if err != nil {
			return nil, err
		}
		interfaces = append(interfaces, ifaces...)
		log.Println("Used interfaces: ", interfaces)
	}

	var failedJoins int
	for _, iface := range interfaces {
		if err := pkConn.JoinGroup(&iface, &net.UDPAddr{IP: mdnsGroupIPv4}); err != nil {
			log.Println("Udp4 JoinGroup failed for iface ", iface)
			failedJoins++
		}
	}
	if failedJoins == len(interfaces) {
		return nil, fmt.Errorf("udp4: failed to join any of these interfaces: %v", interfaces)
	}

	return udpConn, nil
}
