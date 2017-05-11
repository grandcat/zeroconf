package zeroconf

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"time"

	"github.com/cenkalti/backoff"
	"github.com/miekg/dns"
)

// IPType specifies the IP traffic the client listens for.
// This does not guarantee that only mDNS entries of this sepcific
// type passes. E.g. typical mDNS packets distributed via IPv4, often contain
// both DNS A and AAAA entries.
type IPType uint8

// Options for IPType.
const (
	IPv4        = 0x01
	IPv6        = 0x02
	IPv4AndIPv6 = (IPv4 | IPv6) //< Default option.
)

type clientOpts struct {
	listenOn IPType
	ifaces   []net.Interface
}

// ClientOption fills the option struct to configure intefaces, etc.
type ClientOption func(*clientOpts)

// SelectIPTraffic selects the type of IP packets (IPv4, IPv6, or both) this
// instance listens for.
// This does not guarantee that only mDNS entries of this sepcific
// type passes. E.g. typical mDNS packets distributed via IPv4, may contain
// both DNS A and AAAA entries.
func SelectIPTraffic(t IPType) ClientOption {
	return func(o *clientOpts) {
		o.listenOn = t
	}
}

// SelectIfaces selects the interfaces to query for mDNS records
func SelectIfaces(ifaces []net.Interface) ClientOption {
	return func(o *clientOpts) {
		o.ifaces = ifaces
	}
}

// Resolver acts as entry point for service lookups and to browse the DNS-SD.
type Resolver struct {
	c *client
}

// NewResolver creates a new resolver and joins the UDP multicast groups to
// listen for mDNS messages.
func NewResolver(options ...ClientOption) (*Resolver, error) {
	// Apply default configuration and load supplied options.
	var conf = clientOpts{
		listenOn: IPv4AndIPv6,
	}
	for _, o := range options {
		if o != nil {
			o(&conf)
		}
	}

	c, err := newClient(conf)
	if err != nil {
		return nil, err
	}
	return &Resolver{
		c: c,
	}, nil
}

// Browse for all services of a given type in a given domain.
func (r *Resolver) Browse(ctx context.Context, service, domain string, entries chan<- *ServiceEntry) error {
	b := Browser{}

	params := defaultParams(service)
	if domain != "" {
		params.Domain = domain
	}
	params.Entries = entries

	b.cache = newEntryCache()
	go r.c.mainloop(ctx, params, b.cache)

	err := r.c.query(params)
	if err != nil {
		_, cancel := context.WithCancel(ctx)
		cancel()
		return err
	}
	// If previous probe was ok, it should be fine now. In case of an error later on,
	// the entries' queue is closed.
	go r.c.periodicQuery(ctx, params)

	return nil
}

// Browsev2 for all services of a given type in a given domain. When  entries are removed, events send to deletedEntries if non-nil.
func (r *Resolver) Browsev2(ctx context.Context, service, domain string, entries, deletedEntries chan<- *ServiceEntry) error {
	b := Browser{}
	params := defaultParams(service)
	if domain != "" {
		params.Domain = domain
	}
	params.Entries = entries
	params.DeletedEntries = deletedEntries

	b.cache = newEntryCache()
	go r.c.mainloop(ctx, params, b.cache)

	err := r.c.query(params)
	if err != nil {
		_, cancel := context.WithCancel(ctx)
		cancel()
		return err
	}
	// If previous probe was ok, it should be fine now. In case of an error later on,
	// the entries' queue is closed.
	go r.c.periodicQuery(ctx, params)

	return nil
}

// Browsev3 for all services of a given type in a given domain.
func (r *Resolver) Browsev3(ctx context.Context, service, domain string, events chan<- *ServiceEvent) (*Browser, error) {
	b := Browser{}

	params := defaultParams(service)
	if domain != "" {
		params.Domain = domain
	}
	params.Entries = nil
	params.Events = events

	b.cache = newEntryCache()
	go r.c.mainloop(ctx, params, b.cache)

	err := r.c.query(params)
	if err != nil {
		_, cancel := context.WithCancel(ctx)
		cancel()
		return nil, err
	}
	// If previous probe was ok, it should be fine now. In case of an error later on,
	// the entries' queue is closed.
	go r.c.periodicQuery(ctx, params)

	return &b, nil
}

// Lookup a specific service by its name and type in a given domain.
func (r *Resolver) Lookup(ctx context.Context, instance, service, domain string, entries chan<- *ServiceEntry) error {
	params := defaultParams(service)
	params.Instance = instance
	if domain != "" {
		params.Domain = domain
	}
	params.Entries = entries

	sentEntries := newEntryCache()
	go r.c.mainloop(ctx, params, sentEntries)

	err := r.c.query(params)
	if err != nil {
		// XXX: replace cancel with own chan for abort on error
		_, cancel := context.WithCancel(ctx)
		cancel()
		return err
	}
	// If previous probe was ok, it should be fine now. In case of an error later on,
	// the entries' queue is closed.
	go r.c.periodicQuery(ctx, params)

	return nil
}

// defaultParams returns a default set of QueryParams.
func defaultParams(service string) *LookupParams {
	return NewLookupParams("", service, "local", make(chan *ServiceEntry))
}

// Client structure encapsulates both IPv4/IPv6 UDP connections.
type client struct {
	ipv4conn *ipv4.PacketConn
	ipv6conn *ipv6.PacketConn
	ifaces   []net.Interface
}

// Client structure constructor
func newClient(opts clientOpts) (*client, error) {
	ifaces := opts.ifaces
	if len(ifaces) == 0 {
		ifaces = listMulticastInterfaces()
	}
	// IPv4 interfaces
	var ipv4conn *ipv4.PacketConn
	if (opts.listenOn & IPv4) > 0 {
		var err error
		ipv4conn, err = joinUdp4Multicast(ifaces)
		if err != nil {
			return nil, err
		}
	}
	// IPv6 interfaces
	var ipv6conn *ipv6.PacketConn
	if (opts.listenOn & IPv6) > 0 {
		var err error
		ipv6conn, err = joinUdp6Multicast(ifaces)
		if err != nil {
			return nil, err
		}
	}

	return &client{
		ipv4conn: ipv4conn,
		ipv6conn: ipv6conn,
		ifaces:   ifaces,
	}, nil
}

func newEntryCache() *entryCache {
	return &entryCache{entries: make(map[string]*ServiceEntry)}
}

func computeExpiryDuration(ec *entryCache) time.Duration {
	var expDuration time.Duration
	entries := ec.entries
	now := time.Now()

	ec.RLock()
	for _, v := range entries {
		if v.refreshTime != nil {
			d := v.refreshTime.Sub(now)
			if expDuration == 0 {
				expDuration = d
			} else if d < expDuration {
				expDuration = d
			}
		}
		if v.expiryTime != nil {
			d := v.expiryTime.Sub(now)

			if expDuration == 0 {
				expDuration = d
			} else if d < expDuration {
				expDuration = d
			}
		}
	}
	ec.RUnlock()
	//log.Printf("computeExpiryDuration next %d nanoseconds", expDuration)
	return expDuration
}

// Start listeners and waits for the shutdown signal from exit channel
func (c *client) mainloop(ctx context.Context, params *LookupParams, sentEntries *entryCache) {
	// create a time and stop it. we have a channel in the loop to look for timeouts
	t := time.NewTimer(time.Second)
	if !t.Stop() {
		<-t.C
	}
	lastQueriedTime := time.Now()

	// start listening for responses
	msgCh := make(chan *dns.Msg, 32)
	if c.ipv4conn != nil {
		go c.recv(ctx, c.ipv4conn, msgCh)
	}
	if c.ipv6conn != nil {
		go c.recv(ctx, c.ipv6conn, msgCh)
	}

	// Iterate through channels from listeners goroutines
	var entries map[string]*ServiceEntry
	for {
		select {
		case <-ctx.Done():
			// Context expired. Notify subscriber that we are done here.
			params.done()
			c.shutdown()
			if !t.Stop() {
				<-t.C
			}
			return
		case <-t.C:
			//log.Printf("timer expired")

			now := time.Now()
			sentEntries.Lock()
			for k, v := range sentEntries.entries {
				if (v.refreshTime != nil) && now.After(*v.refreshTime) {
					//log.Printf("quering %s because of TTL refresh time expiry", k)

					// query only if my previous query is older than a second
					// If many entries are getting refreshed in a short time this should prevent flood to the network
					if lastQueriedTime.Add(1 * time.Second).Before(time.Now()) {
						c.query(params)
						lastQueriedTime = time.Now()
					}
					v.refreshTime = nil
				}

				if (v.expiryTime != nil) && now.After(*v.expiryTime) {
					//log.Printf("deleting %s because of TTL expiry", k)
					delete(sentEntries.entries, k)

					if params.DeletedEntries != nil {
						params.DeletedEntries <- v
					}

					if params.Events != nil {
						evt := ServiceEvent{ServiceEntry: *v, EventType: Removed}
						params.Events <- &evt
					}
				}
			}
			sentEntries.Unlock()
			nextExpiryDuration := computeExpiryDuration(sentEntries)
			if nextExpiryDuration != 0 {
				t.Reset(nextExpiryDuration)
			}

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
				}
			}
			// Associate IPs in a second round as other fields should be filled by now.
			for _, answer := range sections {
				switch rr := answer.(type) {
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
			sentEntries.Lock()
			for k, e := range entries {
				if e.TTL == 0 {
					//log.Printf("Received TTL==0 entry for %s", k)
					if v, ok := sentEntries.entries[k]; ok {
						// Expire the entry after 1 second. No need to refresh Entry with a query
						et := time.Now().Add(1 * time.Second)
						v.expiryTime = &et
						v.refreshTime = nil
					}
					continue
				}
				if v, ok := sentEntries.entries[k]; ok {
					var rt, et time.Time
					var frac float64

					// random number between 75-85% of TTL expiry time. convert to float64 to overcome rounding errors
					frac = (0.75 + rand.Float64()*0.1) * float64(e.TTL) * float64(time.Second)
					rt = time.Now().Add(time.Duration(frac))
					v.refreshTime = &rt

					et = time.Now().Add(time.Duration(e.TTL) * time.Second)
					v.expiryTime = &et
					continue
				}
				// Require at least one resolved IP address for ServiceEntry
				// TODO: wait some more time as chances are high both will arrive.
				if len(e.AddrIPv4) == 0 && len(e.AddrIPv6) == 0 {
					continue
				}
				// Submit entry to subscriber and cache it.
				// This is also a point to possibly stop probing actively for a
				// service entry.
				if params.Entries != nil {
					params.Entries <- e
				}
				if params.Events != nil {
					evt := ServiceEvent{ServiceEntry: *e, EventType: NewOrUpdated}
					params.Events <- &evt
				}
				sentEntries.entries[k] = e
				params.disableProbing()
			}
			sentEntries.Unlock()

			nextExpiryDuration := computeExpiryDuration(sentEntries)
			if nextExpiryDuration != 0 {
				t.Stop()
				t.Reset(nextExpiryDuration)
			}
			// reset entries
			entries = make(map[string]*ServiceEntry)
		}
	}
}

// Shutdown client will close currently open connections and channel implicitly.
func (c *client) shutdown() {
	if c.ipv4conn != nil {
		c.ipv4conn.Close()
	}
	if c.ipv6conn != nil {
		c.ipv6conn.Close()
	}
}

// Data receiving routine reads from connection, unpacks packets into dns.Msg
// structures and sends them to a given msgCh channel
func (c *client) recv(ctx context.Context, l interface{}, msgCh chan *dns.Msg) {
	var readFrom func([]byte) (n int, src net.Addr, err error)

	switch pConn := l.(type) {
	case *ipv6.PacketConn:
		readFrom = func(b []byte) (n int, src net.Addr, err error) {
			n, _, src, err = pConn.ReadFrom(b)
			return
		}
	case *ipv4.PacketConn:
		readFrom = func(b []byte) (n int, src net.Addr, err error) {
			n, _, src, err = pConn.ReadFrom(b)
			return
		}

	default:
		return
	}

	buf := make([]byte, 65536)
	var fatalErr error
	for {
		// Handles the following cases:
		// - ReadFrom aborts with error due to closed UDP connection -> causes ctx cancel
		// - ReadFrom aborts otherwise.
		// TODO: the context check can be removed. Verify!
		if ctx.Err() != nil || fatalErr != nil {
			return
		}

		n, _, err := readFrom(buf)
		if err != nil {
			fatalErr = err
			continue
		}
		msg := new(dns.Msg)
		if err := msg.Unpack(buf[:n]); err != nil {
			log.Printf("[WARN] mdns: Failed to unpack packet: %v", err)
			continue
		}
		select {
		case msgCh <- msg:
			// Submit decoded DNS message and continue.
		case <-ctx.Done():
			// Abort.
			return
		}
	}
}

// periodicQuery sens multiple probes until a valid response is received by
// the main processing loop or some timeout/cancel fires.
// TODO: move error reporting to shutdown function as periodicQuery is called from
// go routine context.
func (c *client) periodicQuery(ctx context.Context, params *LookupParams) error {
	if params.stopProbing == nil {
		return nil
	}

	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 4 * time.Second
	bo.MaxInterval = 60 * time.Second
	bo.Reset()

	for {
		// Do periodic query.
		if err := c.query(params); err != nil {
			// XXX: use own error handling instead of misuse of context
			_, cancel := context.WithCancel(ctx)
			cancel()
			return err
		}

		// Backoff and cancel logic.
		wait := bo.NextBackOff()
		if wait == backoff.Stop {
			log.Println("periodicQuery: abort due to timeout")
			return nil
		}
		select {
		case <-time.After(wait):
			// Wait for next iteration.
		case <-params.stopProbing:
			// Chan is closed (or happened in the past).
			// Done here. Received a matching mDNS entry.
			return nil
		case <-ctx.Done():
			return ctx.Err()

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
		var wcm ipv4.ControlMessage
		for ifi := range c.ifaces {
			wcm.IfIndex = c.ifaces[ifi].Index
			c.ipv4conn.WriteTo(buf, &wcm, ipv4Addr)
		}
	}
	if c.ipv6conn != nil {
		var wcm ipv6.ControlMessage
		for ifi := range c.ifaces {
			wcm.IfIndex = c.ifaces[ifi].Index
			c.ipv6conn.WriteTo(buf, &wcm, ipv6Addr)
		}
	}
	return nil
}
