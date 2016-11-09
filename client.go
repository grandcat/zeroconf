package bonjour

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/miekg/dns"
)

// Main client data structure to run browse/lookup queries
type Resolver struct {
	c    *client
	Exit chan<- bool
}

// Resolver structure constructor
func NewResolver(ifaces []net.Interface) (*Resolver, error) {
	c, err := newClient(ifaces)
	if err != nil {
		return nil, err
	}
	return &Resolver{c, c.closedCh}, nil
}

// Browse for all services of a given type in a given domain
func (r *Resolver) Browse(service, domain string, entries chan<- *ServiceEntry) error {
	params := defaultParams(service)
	if domain != "" {
		params.Domain = domain
	}
	params.Entries = entries

	go r.c.mainloop(params)

	err := r.c.query(params)
	if err != nil {
		r.Exit <- true
		return err
	}

	return nil
}

// Look up a specific service by its name and type in a given domain
func (r *Resolver) Lookup(instance, service, domain string, entries chan<- *ServiceEntry) error {
	params := defaultParams(service)
	params.Instance = instance
	if domain != "" {
		params.Domain = domain
	}
	params.Entries = entries

	go r.c.mainloop(params)

	err := r.c.query(params)
	if err != nil {
		r.Exit <- true
		return err
	}

	return nil
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
	closedCh  chan bool
	closeLock sync.Mutex
}

// Client structure constructor
func newClient(ifaces []net.Interface) (*client, error) {
	ipv4conn, err := joinUdp4Multicast(ifaces)
	if err != nil {
		return nil, err
	}
	ipv6conn, err := joinUdp6Multicast(ifaces)
	if err != nil {
		return nil, err
	}

	c := &client{
		ipv4conn: ipv4conn,
		ipv6conn: ipv6conn,
		closedCh: make(chan bool),
	}

	return c, nil
}

// Start listeners and waits for the shutdown signal from exit channel
func (c *client) mainloop(params *LookupParams) {
	// start listening for responses
	msgCh := make(chan *dns.Msg, 32)
	if c.ipv4conn != nil {
		go c.recv(c.ipv4conn, msgCh)
	}
	if c.ipv6conn != nil {
		go c.recv(c.ipv6conn, msgCh)
	}

	// Iterate through channels from listeners goroutines
	var entries, sentEntries map[string]*ServiceEntry
	sentEntries = make(map[string]*ServiceEntry)
	for !c.closed {
		select {
		case <-c.closedCh:
			c.shutdown()
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
						if e.HostName == rr.Hdr.Name {
							entries[k].AddrIPv4 = append(entries[k].AddrIPv4, rr.A)
						}
					}
				case *dns.AAAA:
					for k, e := range entries {
						if e.HostName == rr.Hdr.Name {
							entries[k].AddrIPv6 = append(entries[k].AddrIPv6, rr.AAAA)
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
				// Require at least one resolved IP address for ServiceEntry
				// TODO: wait some more time as chances are high both will arrive.
				if len(e.AddrIPv4) == 0 && len(e.AddrIPv6) == 0 {
					continue
				}
				params.Entries <- e
				sentEntries[k] = e
			}
			// reset entries
			entries = make(map[string]*ServiceEntry)
		}
	}
}

// Shutdown client will close currently open connections & channel
func (c *client) shutdown() {
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

	// send the query
	m := new(dns.Msg)
	if serviceInstanceName != "" {
		m.Question = []dns.Question{
			dns.Question{serviceInstanceName, dns.TypeSRV, dns.ClassINET},
			dns.Question{serviceInstanceName, dns.TypeTXT, dns.ClassINET},
		}
		m.RecursionDesired = false
	} else {
		m.SetQuestion(serviceName, dns.TypePTR)
		m.RecursionDesired = false
	}
	if err := c.sendQuery(m); err != nil {
		return err
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
	if c.ipv6conn != nil {
		c.ipv6conn.WriteTo(buf, ipv6Addr)
	}
	return nil
}
