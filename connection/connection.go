package connection

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/miekg/dns"
	"github.com/pkg/errors"
	"github.com/uber-go/multierr"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var (
	// Multicast groups used by mDNS
	mdnsGroupIPv4 = net.IPv4(224, 0, 0, 251)
	mdnsGroupIPv6 = net.ParseIP("ff02::fb")

	// mDNS wildcard addresses
	mdnsWildcardA = &net.UDPAddr{
		IP:   net.ParseIP("224.0.0.0"),
		Port: 5353,
	}
	mdnsWildcardAAAA = &net.UDPAddr{
		IP: net.ParseIP("ff02::"),
		// IP:   net.ParseIP("fd00::12d3:26e7:48db:e7d"),
		Port: 5353,
	}

	// mDNS endpoint addresses
	Ipv4Addr = &net.UDPAddr{
		IP:   mdnsGroupIPv4,
		Port: 5353,
	}
	Ipv6Addr = &net.UDPAddr{
		IP:   mdnsGroupIPv6,
		Port: 5353,
	}
)

// IPType specifies the IP traffic the client listens for.
// This does not guarantee that only mDNS entries of this sepcific
// type passes. E.g. typical mDNS packets distributed via IPv4, often contain
// both DNS A and AAAA entries.
type IPType uint8

func (self *IPType) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case string:
		switch value {
		case "IPv4":
			i := IPType(IPv4)
			self = &i
		case "IPv6":
			i := IPType(IPv6)
			self = &i
		case "IPv4AndIPv6":
			i := IPType(IPv4AndIPv6)
			self = &i
		default:
			return errors.Errorf("invalid ip selection:%v", value)
		}
	case int:
		i := IPType(value)
		self = &i
	}
	return nil
}

const (
	DNSSDInstance           = "_services"
	DNSSDService            = "_dns-sd._udp"
	DNSSDQuestion           = DNSSDInstance + "." + DNSSDService + "."
	QClassCacheFlush uint16 = 1 << 15

	// Options for IPType.
	IPv4        = 0x01
	IPv6        = 0x02
	IPv4AndIPv6 = (IPv4 | IPv6) //< Default option.
)

// ServiceEntry represents a browse/lookup result for client API.
// It is also used to configure service registration (server API), which is
// used to answer multicast queries.
type Service struct {
	Instance string   // Description of the service(e.g. "Service the offers proxy gateway")
	Name     string   // The full name instance + service + domain
	Subtypes []string // Service subtypes
	Domain   string   // If blank, assumes "local"
	Target   string   // The target of the service. In most cases the current hostname.
	Port     uint16   // Service Port
	TXT      []string // Service info served as a TXT record
	TTL      uint32   // TTL of the service record
	A        []net.IP // Host machine IPv4 address
	AAAA     []net.IP // Host machine IPv6 address
}

func NewService(instance, name, domain, target string, port uint16, text []string) (*Service, error) {
	name, subtypes := parseSubtypes(name)

	var err error

	s := &Service{
		Instance: instance,
		Name:     name,
		Domain:   domain,
		Port:     port,
		TXT:      text,
		TTL:      3200,
	}

	if s.Domain == "" {
		s.Domain = "local"
	}

	if target == "" {
		s.Target, err = os.Hostname()
		if err != nil {
			return nil, errors.Errorf("Could not determine host")
		}
	}

	for _, subtype := range subtypes {
		s.Subtypes = append(s.Subtypes, fmt.Sprintf("%s._sub.%s", subtype, name))
	}

	if len(s.A) == 0 || len(s.AAAA) == 0 {
		s, err = AddServiceIP(log.NewNopLogger(), s)
		if err != nil {
			return nil, err
		}
	}

	return s, nil
}

func (self *Service) NameFQDN() string {
	if strings.HasSuffix(self.Name, ".") {
		return self.Name + self.DomainFQDN()
	}
	return self.Name + "." + self.DomainFQDN()
}

func (self *Service) DomainFQDN() string {
	if strings.HasSuffix(self.Domain, ".") {
		return self.Domain
	}
	return self.Domain + "."
}

func (self *Service) InstanceNameFQDN() string {
	if strings.HasSuffix(self.Instance, ".") {
		return self.Instance + self.NameFQDN()
	}
	return self.Instance + "." + self.NameFQDN()
}

func (self *Service) TargetFQDN() string {
	if strings.HasSuffix(self.Target, ".") {
		return self.Target
	}
	return self.Target + "."
}

func JoinUdp6Multicast(logger log.Logger, ifFilter []int) (*ipv6.PacketConn, error) {
	udpConn, err := net.ListenUDP("udp6", mdnsWildcardAAAA)
	if err != nil {
		return nil, err
	}

	// Join multicast groups to receive announcements
	pkConn := ipv6.NewPacketConn(udpConn)
	if err := pkConn.SetControlMessage(ipv6.FlagInterface, true); err != nil {
		level.Error(logger).Log("msg", "http6 SetControlMessage", "err", err)

	}

	interfaces := ListMulticastInterfaces(logger, ifFilter)

	if len(interfaces) == 0 {
		return nil, errors.New("no listed interfaces")
	}
	level.Info(logger).Log("msg", "using multicast", "type", "udp6", "interfaces", fmt.Sprintf("%+v", interfaces))

	var failedJoins int
	for _, iface := range interfaces {
		if err := pkConn.JoinGroup(&iface, &net.UDPAddr{IP: mdnsGroupIPv6}); err != nil {
			level.Error(logger).Log("msg", "Udp6 JoinGroup for iface", "iface", iface, "err", err)
			failedJoins++
		}
	}
	if failedJoins == len(interfaces) {
		pkConn.Close()
		return nil, errors.Errorf("udp6: failed to join any of these interfaces: %v", interfaces)
	}

	return pkConn, nil
}

func JoinUdp4Multicast(logger log.Logger, ifFilter []int) (*ipv4.PacketConn, error) {
	udpConn, err := net.ListenUDP("udp4", mdnsWildcardA)
	if err != nil {
		return nil, err
	}

	// Join multicast groups to receive announcements
	pkConn := ipv4.NewPacketConn(udpConn)
	if err := pkConn.SetControlMessage(ipv4.FlagInterface, true); err != nil {
		return nil, errors.Wrap(err, "http4: set control message")
	}

	interfaces := ListMulticastInterfaces(logger, ifFilter)
	level.Info(logger).Log("msg", "using multicast", "type", "udp6", "interfaces", fmt.Sprintf("%+v", interfaces))

	var failedJoins int
	for _, iface := range interfaces {
		if err := pkConn.JoinGroup(&iface, &net.UDPAddr{IP: mdnsGroupIPv4}); err != nil {
			level.Error(logger).Log("msg", "udp4 JoinGroup for iface", "iface", iface, "err", err)
			failedJoins++
		}
	}
	if failedJoins == len(interfaces) {
		pkConn.Close()
		return nil, errors.Errorf("udp4: failed to join any of these interfaces: %v", interfaces)
	}

	return pkConn, nil
}

func ListMulticastInterfaces(logger log.Logger, filters []int) []net.Interface {
	var interfaces []net.Interface
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

Ifaces:
	for _, ifi := range ifaces {
		for _, filter := range filters {
			if ifi.Index != filter {
				continue Ifaces
			}
		}
		if (ifi.Flags & net.FlagUp) == 0 {
			continue
		}
		if (ifi.Flags & net.FlagMulticast) > 0 {
			interfaces = append(interfaces, ifi)
		}
	}

	return interfaces
}

// MulticastMsg is used to send a multicast response packet
func MulticastMsg(
	logger log.Logger,
	msg *dns.Msg,
	ifaces []net.Interface,
	ipv4conn *ipv4.PacketConn,
	ipv6conn *ipv6.PacketConn,
) error {
	buf, err := msg.Pack()
	if err != nil {
		return err
	}

	if ipv4conn != nil {
		var wcm ipv4.ControlMessage
		for _, intf := range ifaces {
			wcm.IfIndex = intf.Index
			_, err = ipv4conn.WriteTo(buf, &wcm, Ipv4Addr)
			if err != nil {
				err = multierr.Append(err, errors.Wrapf(err, "writing to interface:%+v", wcm.Dst))
			}

		}
	}

	if ipv6conn != nil {
		var wcm ipv6.ControlMessage
		for _, intf := range ifaces {
			wcm.IfIndex = intf.Index
			_, err = ipv6conn.WriteTo(buf, &wcm, Ipv6Addr)
			if err != nil {
				err = multierr.Append(err, errors.Wrapf(err, "writing to interface:%+v", wcm.Dst))
			}
		}
	}
	return err
}

func IsUnicastQuestion(q dns.Question) bool {
	// From RFC6762
	// 18.12.  Repurposing of Top Bit of qclass in Question Section
	//
	//    In the Question Section of a Multicast DNS query, the top bit of the
	//    qclass field is used to indicate that unicast responses are preferred
	//    for this particular question.  (See Section 5.4.)
	return q.Qclass&QClassCacheFlush != 0
}

// UnicastMsg is used to send a unicast response packet
func UnicastMsg(
	logger log.Logger,
	msg *dns.Msg,
	ifaces []net.Interface,
	ipv4conn *ipv4.PacketConn,
	ipv6conn *ipv6.PacketConn,
	from net.Addr,
) error {
	buf, err := msg.Pack()
	if err != nil {
		return err
	}
	addr, ok := from.(*net.UDPAddr)
	if !ok {
		return errors.New("address is not UDP")
	}
	if addr.IP.To4() != nil {
		if ipv4conn == nil {
			return errors.New("from address is IP4 but no ip4 connection provided")
		}
		var err error
		for _, iface := range ifaces {
			var wcm ipv4.ControlMessage
			wcm.IfIndex = iface.Index
			_, e := ipv4conn.WriteTo(buf, &wcm, addr)
			err = multierr.Append(err, e)
		}
		return err
	} else {
		if ipv6conn == nil {
			return errors.New("from address is IP4 but no ip4 connection provided")
		}
		var err error
		for _, iface := range ifaces {
			var wcm ipv6.ControlMessage
			wcm.IfIndex = iface.Index
			_, e := ipv6conn.WriteTo(buf, &wcm, addr)
			err = multierr.Append(err, e)
		}
		return err
	}
}

func AddServiceIP(logger log.Logger, service *Service) (*Service, error) {
	for _, iface := range ListMulticastInterfaces(logger, nil) {
		v4, v6 := AddrsForInterface(&iface)
		service.A = append(service.A, v4...)
		service.AAAA = append(service.AAAA, v6...)
	}
	if service.A == nil && service.AAAA == nil {
		return nil, errors.Errorf("Could not determine host IP addresses")
	}
	return service, nil
}

func AddrsForInterface(iface *net.Interface) ([]net.IP, []net.IP) {
	var v4, v6, v6local []net.IP
	addrs, _ := iface.Addrs()
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				v4 = append(v4, ipnet.IP.To4())
			} else {
				switch ip := ipnet.IP.To16(); ip != nil {
				case ip.IsGlobalUnicast():
					v6 = append(v6, ipnet.IP)
				case ip.IsLinkLocalUnicast():
					v6local = append(v6local, ipnet.IP)
				}
			}
		}
	}
	if len(v6) == 0 {
		v6 = v6local
	}
	return v4, v6
}

func parseSubtypes(service string) (string, []string) {
	subtypes := strings.Split(service, ",")
	return subtypes[0], subtypes[1:]
}
