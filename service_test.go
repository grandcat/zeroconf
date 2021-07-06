package zeroconf

import (
	"context"
	"log"
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

	entries := make(chan *ServiceEntry, 100)
	if err := Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
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
	// before register, mdns resolve shuold not have any entry
	entries := make(chan *ServiceEntry)
	go func(results <-chan *ServiceEntry) {
		s := <-results
		if s != nil {
			t.Errorf("Expected empty service entries but got %v", *s)
		}
	}(entries)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
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

		entries := make(chan *ServiceEntry, 100)
		if err := Browse(ctx, mdnsSubtype, mdnsDomain, entries); err != nil {
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

		entries := make(chan *ServiceEntry, 100)
		if err := Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
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
