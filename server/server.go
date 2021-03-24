package server

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grandcat/zeroconf/connection"
	"github.com/miekg/dns"
	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// Number of Multicast responses sent for a query message (default: 1 < x < 9)
	multicastRepetitions = 1
)

// Server structure encapsulates both IPv4/IPv6 UDP connections
type Server struct {
	services []*connection.Service
	ipv4conn *ipv4.PacketConn
	ipv6conn *ipv6.PacketConn
	config   Config
	ttl      uint32
	ctx      context.Context
	close    context.CancelFunc
	logger   log.Logger
	mtx      sync.Mutex
}

type Config struct {
	IPProtocol connection.IPType
	IfIndex    []int
}

// New creates a server instance.
func New(ctx context.Context, logger log.Logger, config Config) (*Server, error) {
	logger = log.With(logger, "component", "server")

	var (
		err4, err6 error
		ipv4conn   *ipv4.PacketConn
		ipv6conn   *ipv6.PacketConn
	)
	if (config.IPProtocol & connection.IPv4) > 0 {
		ipv4conn, err4 = connection.JoinUdp4Multicast(logger, config.IfIndex)
		if err4 != nil {
			level.Error(logger).Log("msg", "JoinUdp4Multicast", "err", err4)
		}
	}
	if (config.IPProtocol & connection.IPv6) > 0 {
		ipv6conn, err6 = connection.JoinUdp6Multicast(logger, config.IfIndex)
		if err6 != nil {
			level.Error(logger).Log("msg", "joinUdp6Multicast", "err", err6)
		}
	}
	if err4 != nil && err6 != nil {
		// No supported interface left.
		return nil, errors.New("No supported interface")
	}

	ctx, close := context.WithCancel(ctx)

	s := &Server{
		ipv4conn: ipv4conn,
		ipv6conn: ipv6conn,
		config:   config,
		ttl:      3200,
		ctx:      ctx,
		close:    close,
		logger:   logger,
	}

	return s, nil
}

func (self *Server) Run() {
	if self.ipv4conn != nil {
		go self.recv4(self.ipv4conn)
	}
	if self.ipv6conn != nil {
		go self.recv6(self.ipv6conn)
	}
	<-self.ctx.Done()
}

// Register registeres a new service.
func (self *Server) Register(services []*connection.Service) error {
	if len(services) == 0 {
		return errors.New("no services provided to register")
	}
	self.mtx.Lock()
	defer self.mtx.Unlock()

	var newServices []connection.Service

	for _, service := range services {
		if err := validateService(service); err != nil {
			return err
		}

		self.services = append(self.services, service)
		newServices = append(newServices, *service)

	}

	self.announce(newServices)

	return nil
}

func validateService(service *connection.Service) error {
	if service.Instance == "" {
		return errors.Errorf("missing service instance name")
	}

	if service.Name == "" {
		return errors.Errorf("missing service name")
	}
	if service.Domain == "" {
		return errors.Errorf("missing domain name")
	}

	if service.Port == 0 {
		return errors.Errorf("missing port")
	}

	if len(service.A) == 0 || len(service.AAAA) == 0 {
		return errors.Errorf("missing A or AAAA record")
	}
	return nil
}

// TTL sets the TTL for DNS replies
func (self *Server) TTL(ttl uint32) {
	self.ttl = ttl
}

// Close the server.
func (self *Server) Stop() {
	self.close()
	if self.ipv4conn != nil {
		if err := self.ipv4conn.Close(); err != nil {
			level.Error(self.logger).Log("msg", "closing ipv4conn", "err", err)
		}
	}
	if self.ipv6conn != nil {
		if err := self.ipv6conn.Close(); err != nil {
			level.Error(self.logger).Log("msg", "closing ipv6conn", "err", err)
		}
	}
	level.Info(self.logger).Log("msg", "stopped")
}

// recv4 is a long running routine to receive packets from an interface
func (self *Server) recv4(c *ipv4.PacketConn) {
	if c == nil {
		return
	}
	buf := make([]byte, 65536)
	for {
		select {
		case <-self.ctx.Done():
			return
		default:
			var ifIndex int
			n, cm, from, err := c.ReadFrom(buf)
			if err != nil {
				continue
			}
			if cm != nil {
				ifIndex = cm.IfIndex
			}
			if err := self.parsePacket(buf[:n], ifIndex, from); err != nil {
				level.Error(self.logger).Log("msg", "handle query", "err", err)
			}
		}
	}
}

// recv6 is a long running routine to receive packets from an interface
func (self *Server) recv6(c *ipv6.PacketConn) {
	buf := make([]byte, 65536)
	for {
		select {
		case <-self.ctx.Done():
			return
		default:
			var ifIndex int
			n, cm, from, err := c.ReadFrom(buf)
			if err != nil {
				continue
			}
			if cm != nil {
				ifIndex = cm.IfIndex
			}
			if err := self.parsePacket(buf[:n], ifIndex, from); err != nil {
				level.Error(self.logger).Log("msg", "parsePacket", "err", err)
			}
		}
	}
}

// parsePacket is used to parse an incoming packet
func (self *Server) parsePacket(packet []byte, ifIndex int, from net.Addr) error {
	var msg dns.Msg
	if err := msg.Unpack(packet); err != nil {
		return errors.Wrap(err, "unpack packet")
	}
	return self.handleQuery(&msg, ifIndex, from)
}

// handleQuery is used to handle an incoming query
func (self *Server) handleQuery(query *dns.Msg, ifIndex int, from net.Addr) error {
	// Ignore questions with authoritative section for now
	if len(query.Ns) > 0 {
		return nil
	}

	// Handle each question
	var err error
	for _, q := range query.Question {
		msg := self.handleQuestion(q, query, ifIndex)
		if err != nil {
			level.Error(self.logger).Log("msg", "handle question", "err", err)
			continue
		}

		if len(msg.Answer) == 0 {
			continue
		}

		ifaces := connection.ListMulticastInterfaces(self.logger, self.config.IfIndex)

		if connection.IsUnicastQuestion(q) {
			if e := connection.UnicastMsg(
				self.logger,
				msg,
				ifaces,
				self.ipv4conn,
				self.ipv6conn,
				from,
			); e != nil {
				err = e
			}
		} else {
			if e := connection.MulticastMsg(
				self.logger,
				msg,
				ifaces,
				self.ipv4conn,
				self.ipv6conn,
			); e != nil {
				err = e
			}
		}
	}
	return err
}

// RFC6762 7.1. Known-Answer Suppression
func (self *Server) isKnownAnswer(answer dns.RR, query *dns.Msg) bool {
	ptrAnswer, ok := answer.(*dns.PTR)
	if !ok {
		return false
	}
	for _, known := range query.Answer {
		hdr := known.Header()
		if hdr.Rrtype != answer.Header().Rrtype {
			continue
		}
		ptrKnown := known.(*dns.PTR)
		if ptrKnown.Ptr == ptrAnswer.Ptr && hdr.Ttl >= ptrAnswer.Hdr.Ttl/2 {
			level.Debug(self.logger).Log("msg", "skipping known answer", "ptr", ptrKnown)
			return true
		}
	}
	return false
}

// handleQuestion is used to handle an incoming question
func (self *Server) handleQuestion(q dns.Question, query *dns.Msg, ifIndex int) *dns.Msg {
	self.mtx.Lock()
	defer self.mtx.Unlock()

	msg := &dns.Msg{}
	msg.SetReply(query)
	msg.Compress = true
	msg.RecursionDesired = false
	msg.Authoritative = true
	msg.Question = nil // RFC6762 section 6 "responses MUST NOT contain any questions"
	msg.Answer = []dns.RR{}
	msg.Extra = []dns.RR{}

	for _, service := range self.services {
		switch q.Name {
		case connection.DNSSDQuestion + service.DomainFQDN():
			addAnswers(*service, msg, self.ttl, true)
		case service.NameFQDN():
			addAnswers(*service, msg, self.ttl, false)
		default:
			// handle matching subtype query
			for _, subtype := range service.Subtypes {
				subtype = fmt.Sprintf("%self._sub.%s", subtype, service.Name)
				if q.Name == subtype {
					addAnswers(*service, msg, self.ttl, true)
				}
			}
		}

		for i, answer := range msg.Answer {
			if self.isKnownAnswer(answer, query) {
				msg.Answer = append(msg.Answer[:i], msg.Answer[i+1:]...)
			}
		}
	}
	return msg
}

// TODO implement cache checking and add connection.QClassCacheFlush only when there are changed.
func addAnswers(service connection.Service, msg *dns.Msg, ttl uint32, browse bool) {
	// From RFC6762
	// The most significant bit of the rrclass for a record in the Answer
	// Section of a response message is the Multicast DNS cache-flush bit
	// and is discussed in more detail below in Section 10.2, "Announcements
	// to Flush Outdated Cache Entries".
	srv := &dns.SRV{
		Hdr: dns.RR_Header{
			Name:   service.InstanceNameFQDN(),
			Rrtype: dns.TypeSRV,
			Class:  dns.ClassINET | connection.QClassCacheFlush,
			Ttl:    ttl,
		},
		Priority: 0,
		Weight:   0,
		Port:     service.Port,
		Target:   service.TargetFQDN(),
	}
	txt := &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   service.InstanceNameFQDN(),
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET | connection.QClassCacheFlush,
			Ttl:    ttl,
		},
		Txt: service.TXT,
	}

	if browse {
		ptr1 := &dns.PTR{
			Hdr: dns.RR_Header{
				Name:   connection.DNSSDQuestion + service.DomainFQDN(),
				Rrtype: dns.TypePTR,
				Class:  dns.ClassINET,
				Ttl:    ttl,
			},
			Ptr: service.NameFQDN(),
		}
		msg.Answer = append(msg.Answer, ptr1)

		ptr2 := &dns.PTR{
			Hdr: dns.RR_Header{
				Name:   service.NameFQDN(),
				Rrtype: dns.TypePTR,
				Class:  dns.ClassINET,
				Ttl:    ttl,
			},
			Ptr: service.InstanceNameFQDN(),
		}

		msg.Extra = append(msg.Extra, ptr2, srv, txt)
		msg.Extra = append(msg.Extra, addrRecs(service, ttl)...)
		for _, subtype := range service.Subtypes {
			msg.Extra = append(msg.Extra,
				&dns.PTR{
					Hdr: dns.RR_Header{
						Name:   subtype,
						Rrtype: dns.TypePTR,
						Class:  dns.ClassINET | connection.QClassCacheFlush,
						Ttl:    ttl,
					},
					Ptr: service.NameFQDN(),
				})
		}
	} else {
		msg.Answer = append(msg.Answer, srv, txt)
		msg.Answer = append(msg.Answer, addrRecs(service, ttl)...)
		for _, subtype := range service.Subtypes {
			msg.Answer = append(msg.Answer,
				&dns.PTR{
					Hdr: dns.RR_Header{
						Name:   subtype,
						Rrtype: dns.TypePTR,
						Class:  dns.ClassINET | connection.QClassCacheFlush,
						Ttl:    ttl,
					},
					Ptr: service.NameFQDN(),
				})
		}
	}

}

// From RFC6762
// The Multicast DNS responder MUST send at least two unsolicited
// responses, one second apart. To provide increased robustness against
// packet loss, a responder MAY send up to eight unsolicited responses,
// provided that the interval between unsolicited responses increases by
// at least a factor of two with every response sent.
func (self *Server) announce(services []connection.Service) {

	// TODO Why send a probe? For name conflict resolution?
	// self.probe(*service)
	msg := new(dns.Msg)
	msg.MsgHdr.Response = true
	// TODO: make response authoritative if we are the publisher
	msg.Compress = true
	msg.Answer = []dns.RR{}
	msg.Extra = []dns.RR{}

	ifaces := connection.ListMulticastInterfaces(self.logger, self.config.IfIndex)

	timeout := 1 * time.Second
	for i := 0; i < multicastRepetitions; i++ {
		for _, service := range services {
			addAnswers(service, msg, self.ttl, true)
		}
		if err := connection.MulticastMsg(
			self.logger,
			msg,
			ifaces,
			self.ipv4conn,
			self.ipv6conn,
		); err != nil {
			level.Error(self.logger).Log("msg", "send announcement", "err", err)
		}
		time.Sleep(timeout)
		timeout *= 2
	}
}

func (self *Server) probe(service connection.Service) {
	msg := new(dns.Msg)
	msg.SetQuestion(service.Name, dns.TypePTR)
	msg.RecursionDesired = false

	srv := &dns.SRV{
		Hdr: dns.RR_Header{
			Name:   service.NameFQDN(),
			Rrtype: dns.TypeSRV,
			Class:  dns.ClassINET,
			Ttl:    self.ttl,
		},
		Priority: 0,
		Weight:   0,
		Port:     service.Port,
		Target:   service.TargetFQDN(),
	}
	txt := &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   service.Name,
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    self.ttl,
		},
		Txt: service.TXT,
	}
	msg.Ns = []dns.RR{srv, txt}

	randomizer := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := 0; i < multicastRepetitions; i++ {
		if err := connection.MulticastMsg(
			self.logger,
			msg,
			connection.ListMulticastInterfaces(self.logger, self.config.IfIndex),
			self.ipv4conn,
			self.ipv6conn,
		); err != nil {
			level.Error(self.logger).Log("msg", "send probe", "err", err)
		}
		time.Sleep(time.Duration(randomizer.Intn(250)) * time.Millisecond)
	}
}

func (self *Server) Unregister() error {
	self.mtx.Lock()
	defer self.mtx.Unlock()

	msg := new(dns.Msg)
	msg.MsgHdr.Response = true
	msg.Answer = []dns.RR{}
	msg.Extra = []dns.RR{}
	for _, service := range self.services {
		addAnswers(*service, msg, 0, true)
	}
	return connection.MulticastMsg(
		self.logger,
		msg,
		connection.ListMulticastInterfaces(self.logger, self.config.IfIndex),
		self.ipv4conn,
		self.ipv6conn,
	)
}

func addrRecs(service connection.Service, ttl uint32) []dns.RR {
	if ttl > 0 {
		// RFC6762 Section 10 says A/AAAA records SHOULD
		// use TTL of 120s, to account for network interface
		// and IP address changes.
		ttl = 120
	}

	var list []dns.RR
	for _, ipv4 := range service.A {
		a := &dns.A{
			Hdr: dns.RR_Header{
				Name:   service.TargetFQDN(),
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET | connection.QClassCacheFlush,
				Ttl:    ttl,
			},
			A: ipv4.To4(),
		}
		list = append(list, a)
	}
	for _, ipv6 := range service.AAAA {
		aaaa := &dns.AAAA{
			Hdr: dns.RR_Header{
				Name:   service.TargetFQDN(),
				Rrtype: dns.TypeAAAA,
				Class:  dns.ClassINET | connection.QClassCacheFlush,
				Ttl:    ttl,
			},
			AAAA: ipv6,
		}
		list = append(list, aaaa)
	}
	return list
}
