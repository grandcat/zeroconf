package client

import (
	"context"
	"net"
	"strings"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grandcat/zeroconf/connection"
	"github.com/grandcat/zeroconf/logger"
	"github.com/miekg/dns"
	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type Config struct {
	IPProtocol connection.IPType
	IfIndex    []int
	LogLevel   string
}

var DefaultConfig = Config{
	IPProtocol: connection.IPv4AndIPv6,
	LogLevel:   "info",
}

// Client structure encapsulates both IPv4/IPv6 UDP connections.
type Client struct {
	ipv4conn    *ipv4.PacketConn
	ipv6conn    *ipv6.PacketConn
	ctx         context.Context
	close       context.CancelFunc
	logger      log.Logger
	mtx         sync.Mutex
	output      chan connection.Service
	input       chan *dns.Msg
	filterByTXT map[string]struct{}
	config      Config
}

// New creates a new client.
func New(ctx context.Context, lgr log.Logger, cfg Config) (*Client, error) {
	lgr, err := logger.ApplyFilter(cfg.LogLevel, lgr)
	if err != nil {
		return nil, err
	}

	lgr = log.With(lgr, "component", "client")

	// IPv4 interfaces
	var ipv4conn *ipv4.PacketConn
	if (cfg.IPProtocol & connection.IPv4) > 0 {
		var err error
		ipv4conn, err = connection.JoinUdp4Multicast(lgr, cfg.IfIndex)
		if err != nil {
			return nil, err
		}
	}
	// IPv6 interfaces
	var ipv6conn *ipv6.PacketConn
	if (cfg.IPProtocol & connection.IPv6) > 0 {
		var err error
		ipv6conn, err = connection.JoinUdp6Multicast(lgr, cfg.IfIndex)
		if err != nil {
			return nil, err
		}
	}

	ctx, close := context.WithCancel(ctx)

	return &Client{
		ipv4conn: ipv4conn,
		ipv6conn: ipv6conn,
		logger:   lgr,
		ctx:      ctx,
		close:    close,
		output:   make(chan connection.Service, 32),
		input:    make(chan *dns.Msg, 32),
		config:   cfg,
	}, nil
}

func (self *Client) Receive() <-chan connection.Service {
	return self.output
}

func (self *Client) Run() {
	if self.ipv4conn != nil {
		go func() {
			if err := self.recv(self.ipv4conn); err != nil {
				level.Error(self.logger).Log("msg", "reading from the ipv4", "err", err)
			}
		}()

	}
	if self.ipv6conn != nil {
		go func() {
			if err := self.recv(self.ipv6conn); err != nil {
				level.Error(self.logger).Log("msg", "reading from the ipv4", "err", err)
			}
		}()
	}

	for {
		select {
		case <-self.ctx.Done():
			return
		case msg := <-self.input:
			for _, service := range processMsg(self.logger, msg) {
				if self.isFiltered(service.TXT) {
					continue
				}
				select {
				case <-self.ctx.Done():
					return
				case self.output <- *service:
				default:
					level.Debug(self.logger).Log("msg", "receiver channel blocked, skipping sending", "service", service.Name)
				}
			}
		}
	}
}

func processMsg(logger log.Logger, msg *dns.Msg) map[string]*connection.Service {

	ptr := make(map[string]string)
	srv := make(map[string]*dns.SRV)
	txt := make(map[string]*dns.TXT)
	a := make(map[string]map[string]struct{}) // Mapping is [ip]target so that the IP addresses are unique per target.
	aaaa := make(map[string]map[string]struct{})

	services := make(map[string]*connection.Service)

	for _, answer := range append(msg.Answer, msg.Extra...) {
		switch rr := answer.(type) {
		case *dns.PTR:
			ptr[rr.Ptr] = rr.Hdr.Name
		case *dns.SRV:
			srv[answer.Header().Name] = rr
		case *dns.TXT:
			txt[answer.Header().Name] = rr
		case *dns.A:
			if _, ok := a[answer.Header().Name]; !ok {
				a[answer.Header().Name] = map[string]struct{}{}
			}
			a[answer.Header().Name][string(rr.A)] = struct{}{}
		case *dns.AAAA:
			if _, ok := aaaa[answer.Header().Name]; !ok {
				aaaa[answer.Header().Name] = map[string]struct{}{}
			}
			aaaa[answer.Header().Name][string(rr.AAAA)] = struct{}{}
		}
	}

	// Remove any children and point the root to the last children.
	// s1:s2 , s2:s3 becomes s1:s3
	for k, v := range ptr {
		if _, ok := ptr[v]; ok {
			delete(ptr, v)
			ptr[k] = v
		}
	}

	for srvcNameFQDN := range ptr {
		s := strings.Split(srvcNameFQDN, ".")
		if len(s) < 4 {
			level.Error(logger).Log("msg", "skipping non FQDN service for PTR record", "service", srvcNameFQDN)
			delete(srv, srvcNameFQDN)
			continue
		}
		services[srvcNameFQDN] = &connection.Service{
			Instance: s[0],
			Name:     strings.Join(s[1:3], "."),
			Domain:   strings.Join(s[3:len(s)-1], "."), // The last element is the dot so don't use it.
		}
		if v, ok := srv[srvcNameFQDN]; ok {
			services[srvcNameFQDN].Port = v.Port
			services[srvcNameFQDN].Target = strings.TrimSuffix(v.Target, ".")
			services[srvcNameFQDN].TTL = v.Hdr.Ttl
		}
		if v, ok := txt[srvcNameFQDN]; ok {
			services[srvcNameFQDN].TXT = v.Txt
		}
	}
	for srvcNameFQDN, v := range srv {
		s := strings.Split(srvcNameFQDN, ".")
		if len(s) < 4 {
			level.Error(logger).Log("msg", "skipping non FQDN service for A record", "service", srvcNameFQDN)
			delete(srv, srvcNameFQDN)
			continue
		}
		if _, ok := a[v.Target]; ok {
			for ip := range a[v.Target] {
				services[srvcNameFQDN].A = append(services[srvcNameFQDN].A, net.IP(ip))
			}
			for ip := range aaaa[v.Target] {
				services[srvcNameFQDN].AAAA = append(services[srvcNameFQDN].AAAA, net.IP(ip))
			}
		}

	}
	return services
}

func (self *Client) SetFilter(filters ...string) {
	self.mtx.Lock()
	defer self.mtx.Unlock()
	self.filterByTXT = make(map[string]struct{})
	for _, filter := range filters {
		self.filterByTXT[filter] = struct{}{}
	}
}

func (self *Client) isFiltered(txts []string) bool {
	self.mtx.Lock()
	defer self.mtx.Unlock()
	if len(self.filterByTXT) == 0 {
		return false
	}

	for _, txt := range txts {
		if _, ok := self.filterByTXT[txt]; !ok {
			return true
		}
	}

	return false
}

// recv reads from connection, unpacks packets into dns.Msg
// structures and sends them to the input channel.
func (self *Client) recv(l interface{}) error {
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
		return errors.Errorf("invalid interface type:%v", pConn)
	}

	buf := make([]byte, 65536)
	for {
		select {
		case <-self.ctx.Done():
			return nil
		default:
		}
		n, _, err := readFrom(buf)
		if err != nil {
			level.Error(self.logger).Log("msg", "buffer read", "err", err)
			continue
		}
		msg := new(dns.Msg)
		if err := msg.Unpack(buf[:n]); err != nil {
			level.Error(self.logger).Log("msg", "msg unpack", "err", err)
			continue
		}
		select {
		case self.input <- msg:
		case <-self.ctx.Done():
			return nil
		}
	}
}

// Pack the dns.Msg and write to available connections (multicast)
func (self *Client) Query(queries []dns.Question) error {
	msg := new(dns.Msg)
	msg.Question = queries

	msg.RecursionDesired = false
	return connection.MulticastMsg(
		self.logger,
		msg,
		connection.ListMulticastInterfaces(self.logger, self.config.IfIndex),
		self.ipv4conn,
		self.ipv6conn,
	)
}

// Shutdown client will close currently open connections and channel implicitly.
func (self *Client) Stop() {
	self.close()
	var err error
	if self.ipv4conn != nil {
		level.Error(self.logger).Log("msg", "closing ipv4conn", "err", err)
	}
	if self.ipv6conn != nil {
		level.Error(self.logger).Log("msg", "closing ipv6conn", "err", err)
	}
}
