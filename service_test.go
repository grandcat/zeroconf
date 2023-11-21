package zeroconf

import (
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
)

var (
	mdnsName    = "test--xxxxxxxxxxxx"
	mdnsService = "_test--xxxx._tcp"
	mdnsSubtype = "_test--xxxx._tcp,_fancy"
	mdnsDomain  = "local."
	mdnsPort    = 8888
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
		t.Fatalf("Expected create resolver success, but got %v", err)
	}
	entries := make(chan *ServiceEntry, 100)
	if err := resolver.Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
		t.Fatalf("Expected browse success, but got %v", err)
	}
	<-ctx.Done()

	if len(entries) != 1 {
		t.Fatalf("Expected number of service entries is 1, but got %d", len(entries))
	}
	result := <-entries
	if result.Domain != mdnsDomain {
		t.Fatalf("Expected domain is %s, but got %s", mdnsDomain, result.Domain)
	}
	if result.Service != mdnsService {
		t.Fatalf("Expected service is %s, but got %s", mdnsService, result.Service)
	}
	if result.Instance != mdnsName {
		t.Fatalf("Expected instance is %s, but got %s", mdnsName, result.Instance)
	}
	if result.Port != mdnsPort {
		t.Fatalf("Expected port is %d, but got %d", mdnsPort, result.Port)
	}
}

func TestNoRegister(t *testing.T) {
	resolver, err := NewResolver(nil)
	if err != nil {
		t.Fatalf("Expected create resolver success, but got %v", err)
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
		t.Fatalf("Expected browse success, but got %v", err)
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
			t.Fatalf("Expected create resolver success, but got %v", err)
		}
		entries := make(chan *ServiceEntry, 100)
		if err := resolver.Browse(ctx, mdnsSubtype, mdnsDomain, entries); err != nil {
			t.Fatalf("Expected browse success, but got %v", err)
		}
		<-ctx.Done()

		if len(entries) != 1 {
			t.Fatalf("Expected number of service entries is 1, but got %d", len(entries))
		}
		result := <-entries
		if result.Domain != mdnsDomain {
			t.Fatalf("Expected domain is %s, but got %s", mdnsDomain, result.Domain)
		}
		if result.Service != mdnsService {
			t.Fatalf("Expected service is %s, but got %s", mdnsService, result.Service)
		}
		if result.Instance != mdnsName {
			t.Fatalf("Expected instance is %s, but got %s", mdnsName, result.Instance)
		}
		if result.Port != mdnsPort {
			t.Fatalf("Expected port is %d, but got %d", mdnsPort, result.Port)
		}
	})

	t.Run("browse without subtype", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go startMDNS(ctx, mdnsPort, mdnsName, mdnsSubtype, mdnsDomain)

		time.Sleep(time.Second)

		resolver, err := NewResolver(nil)
		if err != nil {
			t.Fatalf("Expected create resolver success, but got %v", err)
		}
		entries := make(chan *ServiceEntry, 100)
		if err := resolver.Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
			t.Fatalf("Expected browse success, but got %v", err)
		}
		<-ctx.Done()

		if len(entries) != 1 {
			t.Fatalf("Expected number of service entries is 1, but got %d", len(entries))
		}
		result := <-entries
		if result.Domain != mdnsDomain {
			t.Fatalf("Expected domain is %s, but got %s", mdnsDomain, result.Domain)
		}
		if result.Service != mdnsService {
			t.Fatalf("Expected service is %s, but got %s", mdnsService, result.Service)
		}
		if result.Instance != mdnsName {
			t.Fatalf("Expected instance is %s, but got %s", mdnsName, result.Instance)
		}
		if result.Port != mdnsPort {
			t.Fatalf("Expected port is %d, but got %d", mdnsPort, result.Port)
		}
	})
}

// Test the default domain is applied.
func TestDefaultDomain(t *testing.T) {
	t.Run("register", func(t *testing.T) {
		server, err := Register(mdnsName, mdnsService, "", mdnsPort, []string{"txtv=0", "lo=2", "la=3"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if server == nil {
			t.Fatal("expect non-nil")
		}
		// Check the service record's cached fields
		sr := server.service.ServiceRecord
		if strings.Contains(sr.serviceName, "..") {
			t.Errorf("malformed service name: %s", sr.serviceName)
		}
		if strings.Contains(sr.serviceInstanceName, "..") {
			t.Errorf("malformed service instance name: %s", sr.serviceInstanceName)
		}

		t.Logf("Published service: %+v", server.service)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Wait for context to time out
		<-ctx.Done()

		t.Log("Shutting down.")
		server.Shutdown()
	})

	t.Run("registerproxy", func(t *testing.T) {
		server, err := RegisterProxy(mdnsName, mdnsService, "", mdnsPort, "localhost", []string{"::1"}, []string{"txtv=0", "lo=2", "la=3"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if server == nil {
			t.Fatal("expect non-nil")
		}
		// Check the service record's cached fields
		sr := server.service.ServiceRecord
		if strings.Contains(sr.serviceName, "..") {
			t.Errorf("malformed service name: %s", sr.serviceName)
		}
		if strings.Contains(sr.serviceInstanceName, "..") {
			t.Errorf("malformed service instance name: %s", sr.serviceInstanceName)
		}

		t.Logf("Published service: %+v", server.service)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Wait for context to time out
		<-ctx.Done()

		t.Log("Shutting down.")
		server.Shutdown()
	})
}

func TestNewRegisterServiceEntry(t *testing.T) {
	tests := []struct {
		name                      string
		instance, service, domain string
		port                      int
		text                      []string
		err                       bool
	}{
		{"minimal", mdnsName, mdnsService, mdnsDomain, mdnsPort, []string{}, false},
		// Required parameters
		{"require-instance", "", mdnsService, mdnsDomain, mdnsPort, []string{}, true},
		{"require-service", mdnsName, "", mdnsDomain, mdnsPort, []string{}, true},
		{"require-port", mdnsName, mdnsService, mdnsDomain, 0, []string{}, true},
		// Default domain
		{"default-domain", mdnsName, mdnsService, "", mdnsPort, []string{}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			se, err := newRegisterServiceEntry(test.instance, test.service, test.domain, test.port, test.text)
			if test.err && err == nil {
				t.Error("expect error")
			} else if !test.err && err != nil {
				t.Error(err)
			}
			if err == nil && se == nil {
				t.Error("expect non-nil")
			}
		})
	}
}
