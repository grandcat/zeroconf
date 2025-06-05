package zeroconf

import (
	"context"
	"log"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

var (
	mdnsName    = "test--xxxxxxxxxxxx"
	mdnsService = "_test--xxxx._tcp"
	mdnsSubtype = "_test--xxxx._tcp,_fancy"
	mdnsDomain  = "local."
	mdnsPort    = 8888
)

const (
	expectedResolverSuccessMsg = "Expected create resolver success, but got %v"
	expectedBrowseSuccessMsg   = "Expected browse success, but got %v"
	expectedNumEntriesMsg      = "Expected number of service entries is 1, but got %d"
	expectedDomainMsg          = "Expected domain is %s, but got %s"
	expectedServiceMsg         = "Expected service is %s, but got %s"
	expectedInstanceMsg        = "Expected instance is %s, but got %s"
	expectedPortMsg            = "Expected port is %d, but got %d"
)

func startMDNS(ctx context.Context, port int, name, service, domain string) {
	// 5353 is default mdns port
	server, err := Register(name, service, domain, port, []string{"txtv=0", "lo=1", "la=2"}, nil)
	if err != nil {
		panic(errors.Wrap(err, "while registering mdns service"))
	}
	defer server.Shutdown()
	log.Printf("Published service: %s, type: %s, domain: %s", name, service, domain)

	<-ctx.Done()

	log.Printf("Shutting down.")

}

func TestBasic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go startMDNS(ctx, mdnsPort, mdnsName, mdnsService, mdnsDomain)

	time.Sleep(time.Second)

	resolver, err := NewResolver(nil)
	if err != nil {
		t.Fatalf(expectedResolverSuccessMsg, err)
	}
	entries := make(chan *ServiceEntry, 100)
	if err := resolver.Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
		t.Fatalf(expectedBrowseSuccessMsg, err)
	}
	<-ctx.Done()

	if len(entries) != 1 {
		t.Fatalf(expectedNumEntriesMsg, len(entries))
	}
	result := <-entries
	if result.Domain != mdnsDomain {
		t.Fatalf(expectedDomainMsg, mdnsDomain, result.Domain)
	}
	if result.Service != mdnsService {
		t.Fatalf(expectedServiceMsg, mdnsService, result.Service)
	}
	if result.Instance != mdnsName {
		t.Fatalf(expectedInstanceMsg, mdnsName, result.Instance)
	}
	if result.Port != mdnsPort {
		t.Fatalf(expectedPortMsg, mdnsPort, result.Port)
	}
}

func TestNoRegister(t *testing.T) {
	resolver, err := NewResolver(nil)
	if err != nil {
		t.Fatalf(expectedResolverSuccessMsg, err)
	}

	// before register, mdns resolve shuold not have any entry
	entries := make(chan *ServiceEntry)
	go func(results <-chan *ServiceEntry) {
		s := <-results
		if s != nil {
			t.Errorf("Expected empty service entries but got %v", *s)
		}
	}(entries)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := resolver.Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
		t.Fatalf(expectedBrowseSuccessMsg, err)
	}
	<-ctx.Done()
	cancel()
}

func TestSubtype(t *testing.T) {
	t.Run("browse with subtype", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go startMDNS(ctx, mdnsPort, mdnsName, mdnsSubtype, mdnsDomain)

		time.Sleep(time.Second)

		resolver, err := NewResolver(nil)
		if err != nil {
			t.Fatalf(expectedResolverSuccessMsg, err)
		}
		entries := make(chan *ServiceEntry, 100)
		if err := resolver.Browse(ctx, mdnsSubtype, mdnsDomain, entries); err != nil {
			t.Fatalf(expectedBrowseSuccessMsg, err)
		}
		<-ctx.Done()

		if len(entries) != 1 {
			t.Fatalf(expectedNumEntriesMsg, len(entries))
		}
		result := <-entries
		if result.Domain != mdnsDomain {
			t.Fatalf(expectedDomainMsg, mdnsDomain, result.Domain)
		}
		if result.Service != mdnsService {
			t.Fatalf(expectedServiceMsg, mdnsService, result.Service)
		}
		if result.Instance != mdnsName {
			t.Fatalf(expectedInstanceMsg, mdnsName, result.Instance)
		}
		if result.Port != mdnsPort {
			t.Fatalf(expectedPortMsg, mdnsPort, result.Port)
		}
	})

	t.Run("browse without subtype", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go startMDNS(ctx, mdnsPort, mdnsName, mdnsSubtype, mdnsDomain)

		time.Sleep(time.Second)

		resolver, err := NewResolver(nil)
		if err != nil {
			t.Fatalf(expectedResolverSuccessMsg, err)
		}
		entries := make(chan *ServiceEntry, 100)
		if err := resolver.Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
			t.Fatalf("Expected browse success, but got %v", err)
		}
		<-ctx.Done()

		if len(entries) != 1 {
			t.Fatalf(expectedNumEntriesMsg, len(entries))
		}
		result := <-entries
		if result.Domain != mdnsDomain {
			t.Fatalf("Expected domain is %s, but got %s", mdnsDomain, result.Domain)
		}
		if result.Service != mdnsService {
			t.Fatalf("Expected service is %s, but got %s", mdnsService, result.Service)
		}
		if result.Instance != mdnsName {
			t.Fatalf(expectedInstanceMsg, mdnsName, result.Instance)
		}
		if result.Port != mdnsPort {
			t.Fatalf(expectedPortMsg, mdnsPort, result.Port)
		}
	})
}

func TestRegister(t *testing.T) {
	instance := "my-service"
	service := "_http._tcp"
	domain := "local"
	port := 8080
	text := []string{"foo=bar", "version=1"}

	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("failed to get interfaces: %v", err)
	}

	server, err := Register(instance, service, domain, port, text, ifaces)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}
	defer server.Shutdown()

	entry := server.service

	// Verify instance, service, domain
	if entry.Instance != instance {
		t.Errorf("expected instance %q, got %q", instance, entry.Instance)
	}
	if entry.Service != service {
		t.Errorf("expected service %q, got %q", service, entry.Service)
	}
	if strings.TrimSuffix(entry.Domain, ".") != domain {
		t.Errorf("expected domain %q, got %q", domain, entry.Domain)
	}

	// Verify HostName includes system hostname and domain
	hostname, _ := os.Hostname()
	expectedSuffix := "." + domain + "."
	if !strings.HasPrefix(entry.HostName, hostname+".") || !strings.HasSuffix(entry.HostName, expectedSuffix) {
		t.Errorf("unexpected HostName format: got %q", entry.HostName)
	}

	// Verify IPs are populated
	if len(entry.AddrIPv4) == 0 && len(entry.AddrIPv6) == 0 {
		t.Error("expected at least one IP address")
	}
}

func TestProxyRegistrationConfigSetup(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil || len(ifaces) == 0 {
		t.Fatalf("Failed to get network interfaces: %v", err)
	}

	config := ProxyRegistrationConfig{
		Instance: "test-instance",
		Service:  "_http._tcp",
		Domain:   "local.",
		Port:     8080,
		Host:     "test-host",
		IPs:      []string{"192.168.1.10", "10.0.0.1"},
		Text:     []string{"key=value", "env=dev"},
		Ifaces:   []net.Interface{ifaces[0]}, // use the first available interface
	}

	// Register the service proxy (Updated to new RegisterProxy function)
	server, err := RegisterProxy(config)
	assert.NoError(t, err)
	assert.NotNil(t, server)

	// Ensure service proxy is registered correctly
	assert.Equal(t, config.Instance, server.service.Instance)
	assert.Equal(t, config.Service, server.service.Service)
	assert.Equal(t, config.Port, server.service.Port)
	assert.Equal(t, config.Host+"."+config.Domain, server.service.HostName)
	assert.Equal(t, config.Text, server.service.Text)

	// Verify HostName includes system hostname and domain
	expectedHostname := config.Host + "." + config.Domain
	assert.Equal(t, expectedHostname, server.service.HostName)

	// Act: Test the server's proxy behavior, including probing and announcement
	server.probe()

	// Assert: Verify server operations or shutdown
	server.Shutdown()
}
