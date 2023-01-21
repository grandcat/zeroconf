package zeroconf

import (
	"os"
	"strings"
	"testing"
)

func TestRegister(t *testing.T) {
	expected := ServiceEntry{
		ServiceRecord: ServiceRecord{
			Instance: "instance",
			Service:  "service",
			Domain:   "domain",
		},
		Port: 1337,
		Text: []string{"txtv=0", "lo=1", "la=2"},
	}
	server, err := Register(expected.Instance, expected.Service, expected.Domain, expected.Port, expected.Text, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.Shutdown()

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	expected.HostName = hostname + ".domain."

	checkServiceEntry(t, *server.service, expected)
}

func TestRegisterProxy(t *testing.T) {
	expected := ServiceEntry{
		ServiceRecord: ServiceRecord{
			Instance: "instance",
			Service:  "service",
			Domain:   "domain",
		},
		Port:     1337,
		HostName: "proxy.domain.",
		Text:     []string{"txtv=0", "lo=1", "la=2"},
	}
	server, err := RegisterProxy(expected.Instance, expected.Service, expected.Domain, expected.Port, expected.HostName, nil, expected.Text, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.Shutdown()

	checkServiceEntry(t, *server.service, expected)
}

func checkServiceEntry(t *testing.T, got, expected ServiceEntry) {
	t.Helper()
	if got.Instance != expected.Instance {
		t.Fatalf("Expected Instance is %s, but got %s", expected.Instance, got.Instance)
	}
	if got.Service != expected.Service {
		t.Fatalf("Expected Service is %s, but got %s", expected.Service, got.Service)
	}
	if got.Domain != expected.Domain {
		t.Fatalf("Expected Domain is %s, but got %s", expected.Domain, got.Domain)
	}
	if got.Port != expected.Port {
		t.Fatalf("Expected Port is %d, but got %d", expected.Port, got.Port)
	}
	if strings.Join(got.Text, "  ") != strings.Join(expected.Text, "  ") {
		t.Fatalf("Expected Text is %s, but got %s", expected.Text, got.Text)
	}
	if got.HostName != expected.HostName {
		t.Fatalf("Expected HostName is %s, but got %s", expected.HostName, got.HostName)
	}
	if len(got.AddrIPv4) == 0 && len(got.AddrIPv6) == 0 {
		t.Fatal("Unexpected empty IPV4 and IPV6")
	}
}
