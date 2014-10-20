package bonjour

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"

	"code.google.com/p/go.net/ipv4"
	"code.google.com/p/go.net/ipv6"
	"github.com/miekg/dns"
)

// Browse for all services of a fiven type in a given domain
func Browse(service, domain string, entries chan<- *ServiceEntry, iface *net.Interface) error {
	params := defaultParams(service)
	if domain != "" {
		params.Domain = domain
	}
	params.Entries = entries

	client, err := newClient(iface)
	if err != nil {
		return err
	}
	defer client.shutdown()

	return client.query(params)
}

// Look up a specific service by its name and type in a given domain
func Lookup(instance, service, domain string, entries chan<- *ServiceEntry, iface *net.Interface) error {
	params := defaultParams(service)
	params.Instance = instance
	if domain != "" {
		params.Domain = domain
	}
	params.Entries = entries

	client, err := newClient(iface)
	if err != nil {
		return err
	}
	defer client.shutdown()

	return client.query(params)
}

// defaultParams is used to return a default set of QueryParam's
func defaultParams(service string) *LookupParams {
	return NewLookupParams("", service, "local", make(chan *ServiceEntry))
}

// Client structure incapsulates both IPv4/IPv6 UDP connections
type client struct {
	ipv4conn  *net.UDPConn
	ipv6conn  *net.UDPConn
	closed    bool
	closedCh  chan struct{}
	closeLock sync.Mutex
}

// Client structure constructor
func newClient(iface *net.Interface) (*client, error) {
	// Create wildcard connections (because :5353 can be already taken by other apps)
	ipv4conn, err := net.ListenUDP("udp4", mdnsWildcardAddrIPv4)
	if err != nil {
		log.Printf("[ERR] bonjour: Failed to bind to udp4 port: %v", err)
	}
	ipv6conn, err := net.ListenUDP("udp6", mdnsWildcardAddrIPv6)
	if err != nil {
		log.Printf("[ERR] bonjour: Failed to bind to udp6 port: %v", err)
	}
	if ipv4conn == nil && ipv6conn == nil {
		return nil, fmt.Errorf("[ERR] bonjour: Failed to bind to any udp port!")
	}

	// Join multicast groups to receive announcements from server
	p1 := ipv4.NewPacketConn(ipv4conn)
	p2 := ipv6.NewPacketConn(ipv6conn)
	if iface != nil {
		if err := p1.JoinGroup(iface, &net.UDPAddr{IP: mdnsGroupIPv4}); err != nil {
			return nil, err
		}
		if err := p2.JoinGroup(iface, &net.UDPAddr{IP: mdnsGroupIPv6}); err != nil {
			return nil, err
		}
	} else {
		ifaces, err := net.Interfaces()
		if err != nil {
			return nil, err
		}
		errCount1, errCount2 := 0, 0
		for _, iface := range ifaces {
			if err := p1.JoinGroup(&iface, &net.UDPAddr{IP: mdnsGroupIPv4}); err != nil {
				errCount1++
			}
			if err := p2.JoinGroup(&iface, &net.UDPAddr{IP: mdnsGroupIPv6}); err != nil {
				errCount2++
			}
		}
		if len(ifaces) == errCount1 && len(ifaces) == errCount2 {
			return nil, fmt.Errorf("Failed to join multicast group on all interfaces!")
		}
	}

	c := &client{
		ipv4conn: ipv4conn,
		ipv6conn: ipv6conn,
		closedCh: make(chan struct{}),
	}

	return c, nil
}

// Shutdown client will close currently open connections & channel
func (c *client) shutdown() {
	log.Println("client.shutdown()")

	c.closeLock.Lock()
	defer c.closeLock.Unlock()

	if c.closed {
		return
	}
	c.closed = true
	close(c.closedCh)

	if c.ipv4conn != nil {
		c.ipv4conn.Close()
	}
	if c.ipv6conn != nil {
		c.ipv6conn.Close()
	}
}

// Data receiving routine reads from connection, unpacks packets into dns.Msg
// structures and sends them to a given msgCh channel
func (c *client) recv(l *net.UDPConn, msgCh chan *dns.Msg) {
	if l == nil {
		return
	}
	buf := make([]byte, 65536)
	for !c.closed {
		n, _, err := l.ReadFrom(buf)
		if err != nil {
			log.Println("[ERR] Error reading from connection:", err.Error())
			continue
		}
		msg := new(dns.Msg)
		if err := msg.Unpack(buf[:n]); err != nil {
			log.Printf("[ERR] mdns: Failed to unpack packet: %v", err)
			continue
		}
		select {
		case msgCh <- msg:
		case <-c.closedCh:
			return
		}
	}
}

// Performs the actual query by service name (browse) or service instance name (lookup),
// start response listeners goroutines and loops over the entries channel.
func (c *client) query(params *LookupParams) error {
	var serviceName, serviceInstanceName string
	serviceName = fmt.Sprintf("%s.%s.", trimDot(params.Service), trimDot(params.Domain))
	if params.Instance != "" {
		serviceInstanceName = fmt.Sprintf("%s.%s", params.Instance, serviceName)
	}

	// start listening for responses
	msgCh := make(chan *dns.Msg, 32)
	go c.recv(c.ipv4conn, msgCh)
	go c.recv(c.ipv6conn, msgCh)

	// send the query
	m := new(dns.Msg)
	if serviceInstanceName != "" {
		m.SetQuestion(serviceInstanceName, dns.TypeSRV)
		m.SetQuestion(serviceInstanceName, dns.TypeTXT)
		m.RecursionDesired = false
	} else {
		m.SetQuestion(serviceName, dns.TypePTR)
		m.RecursionDesired = false
	}
	if err := c.sendQuery(m); err != nil {
		return nil
	}

	// Iterate through channels from listeners goroutines
	var entries, sentEntries map[string]*ServiceEntry
	sentEntries = make(map[string]*ServiceEntry)
	for {
		select {
		case msg := <-msgCh:
			entries = make(map[string]*ServiceEntry)
			sections := append(msg.Answer, msg.Ns...)
			sections = append(sections, msg.Extra...)
			for _, answer := range sections {
				switch rr := answer.(type) {
				case *dns.PTR:
					if params.ServiceName() != rr.Hdr.Name {
						continue
					}
					if params.ServiceInstanceName() != "" && params.ServiceInstanceName() != rr.Ptr {
						continue
					}
					if _, ok := entries[rr.Ptr]; !ok {
						entries[rr.Ptr] = NewServiceEntry(
							trimDot(strings.Replace(rr.Ptr, rr.Hdr.Name, "", -1)),
							params.Service,
							params.Domain)
					}
					entries[rr.Ptr].TTL = rr.Hdr.Ttl
				case *dns.SRV:
					if params.ServiceInstanceName() != "" && params.ServiceInstanceName() != rr.Hdr.Name {
						continue
					} else if !strings.HasSuffix(rr.Hdr.Name, params.ServiceName()) {
						continue
					}
					if _, ok := entries[rr.Hdr.Name]; !ok {
						entries[rr.Hdr.Name] = NewServiceEntry(
							trimDot(strings.Replace(rr.Hdr.Name, params.ServiceName(), "", 1)),
							params.Service,
							params.Domain)
					}
					entries[rr.Hdr.Name].HostName = rr.Target
					entries[rr.Hdr.Name].Port = int(rr.Port)
					entries[rr.Hdr.Name].TTL = rr.Hdr.Ttl
				case *dns.TXT:
					if params.ServiceInstanceName() != "" && params.ServiceInstanceName() != rr.Hdr.Name {
						continue
					} else if !strings.HasSuffix(rr.Hdr.Name, params.ServiceName()) {
						continue
					}
					if _, ok := entries[rr.Hdr.Name]; !ok {
						entries[rr.Hdr.Name] = NewServiceEntry(
							trimDot(strings.Replace(rr.Hdr.Name, params.ServiceName(), "", 1)),
							params.Service,
							params.Domain)
					}
					entries[rr.Hdr.Name].Text = rr.Txt
					entries[rr.Hdr.Name].TTL = rr.Hdr.Ttl
				case *dns.A:
					for k, e := range entries {
						if e.HostName == rr.Hdr.Name && entries[k].AddrIPv4 == nil {
							entries[k].AddrIPv4 = rr.A
						}
					}
				case *dns.AAAA:
					for k, e := range entries {
						if e.HostName == rr.Hdr.Name && entries[k].AddrIPv6 == nil {
							entries[k].AddrIPv6 = rr.AAAA
						}
					}
				}
			}
		}
		if len(entries) > 0 {
			for k, e := range entries {
				if e.TTL == 0 {
					delete(entries, k)
					delete(sentEntries, k)
					continue
				}
				if _, ok := sentEntries[k]; ok {
					continue
				}
				params.Entries <- e
				sentEntries[k] = e
			}
		}
	}

	return nil
}

// Pack the dns.Msg and write to available connections (multicast)
func (c *client) sendQuery(msg *dns.Msg) error {
	buf, err := msg.Pack()
	if err != nil {
		return err
	}
	if c.ipv4conn != nil {
		c.ipv4conn.WriteTo(buf, ipv4Addr)
	}
	if c.ipv4conn != nil {
		c.ipv4conn.WriteTo(buf, ipv6Addr)
	}
	return nil
}
