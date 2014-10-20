package bonjour

import (
	"fmt"
	"net"
)

// Basic service description that contains instance name, service type & domain
type ServiceRecord struct {
	Instance string // Instance name (e.g. "My web page")
	Service  string // Service name (e.g. _http._tcp.)
	Domain   string // If blank, assumes "local"

	// private variable populated on the first call to ServiceName()/ServiceInstanceName()
	serviceName         string
	serviceInstanceName string
}

// Returns complete service name (e.g. _foobar._tcp.local.)
func (s *ServiceRecord) ServiceName() string {
	if s.serviceName == "" {
		s.serviceName = fmt.Sprintf("%s.%s.", trimDot(s.Service), trimDot(s.Domain))
	}
	return s.serviceName
}

// Returns complete service instance name (e.g. MyDemo\ Service._foobar._tcp.local.)
func (s *ServiceRecord) ServiceInstanceName() string {
	// If no instance name provided we cannot compose service instance name
	if s.Instance == "" {
		return ""
	}
	// If not cached - compose and cache
	if s.serviceInstanceName == "" {
		s.serviceInstanceName = fmt.Sprintf("%s.%s", trimDot(s.Instance), s.ServiceName())
	}
	return s.serviceInstanceName
}

func NewServiceRecord(instance, service, domain string) *ServiceRecord {
	return &ServiceRecord{instance, service, domain, "", ""}
}

// LookupParams contains configurable properties to create a service discovery request
type LookupParams struct {
	ServiceRecord
	Entries chan<- *ServiceEntry // Entries Channel
}

func NewLookupParams(instance, service, domain string, entries chan<- *ServiceEntry) *LookupParams {
	return &LookupParams{
		*NewServiceRecord(instance, service, domain),
		entries,
	}
}

type ServiceEntry struct {
	ServiceRecord
	HostName string   // Host machine DNS name
	Port     int      // Service Port
	Text     []string // Service info served as a TXT record
	TTL      uint32   // TTL of the service record
	AddrIPv4 net.IP   // Host machine IPv4 address
	AddrIPv6 net.IP   // Host machine IPv6 address
}

func NewServiceEntry(instance, service, domain string) *ServiceEntry {
	return &ServiceEntry{
		*NewServiceRecord(instance, service, domain),
		"",
		0,
		[]string{},
		0,
		nil,
		nil,
	}
}
