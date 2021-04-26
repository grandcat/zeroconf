package zeroconf

import (
	"context"
	"fmt"
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
	defer func() {
		if err := server.Shutdown(); err != nil {
			panic(errors.Wrap(err, "while shutting mdns service"))
		}
	}()
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
	entries := make(chan *ServiceEntry)
	expectedResult := []*ServiceEntry{}
	go func(results <-chan *ServiceEntry) {
		s := <-results
		expectedResult = append(expectedResult, s)
	}(entries)

	if err := resolver.Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
		t.Fatalf("Expected browse success, but got %v", err)
	}
	<-ctx.Done()

	if len(expectedResult) != 1 {
		t.Fatalf("Expected number of service entries is 1, but got %d", len(expectedResult))
	}
	if expectedResult[0].Domain != mdnsDomain {
		t.Fatalf("Expected domain is %s, but got %s", mdnsDomain, expectedResult[0].Domain)
	}
	if expectedResult[0].Service != mdnsService {
		t.Fatalf("Expected service is %s, but got %s", mdnsService, expectedResult[0].Service)
	}
	if expectedResult[0].Instance != mdnsName {
		t.Fatalf("Expected instance is %s, but got %s", mdnsName, expectedResult[0].Instance)
	}
	if expectedResult[0].Port != mdnsPort {
		t.Fatalf("Expected port is %d, but got %d", mdnsPort, expectedResult[0].Port)
	}
}

func TestNoRegister(t *testing.T) {
	resolver, err := NewResolver(nil)
	if err != nil {
		t.Fatalf("Expected create resolver success, but got %v", err)
	}

	// before register, mdns resolve should not have any entry
	entries := make(chan *ServiceEntry)
	var resultError error
	go func(results <-chan *ServiceEntry) {
		s := <-results
		if s != nil {
			resultError = fmt.Errorf("Expected empty service entries but got %v", *s)
		}
	}(entries)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := resolver.Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
		t.Fatalf("Expected browse success, but got %v", err)
	}
	<-ctx.Done()
	cancel()
	if resultError != nil {
		t.Fatal(resultError)
	}
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
		entries := make(chan *ServiceEntry)
		var expectedResult []*ServiceEntry
		go func(results <-chan *ServiceEntry) {
			s := <-results
			expectedResult = append(expectedResult, s)
		}(entries)

		if err := resolver.Browse(ctx, mdnsSubtype, mdnsDomain, entries); err != nil {
			t.Fatalf("Expected browse success, but got %v", err)
		}
		<-ctx.Done()

		if len(expectedResult) != 1 {
			t.Fatalf("Expected number of service entries is 1, but got %d", len(expectedResult))
		}
		if expectedResult[0].Domain != mdnsDomain {
			t.Fatalf("Expected domain is %s, but got %s", mdnsDomain, expectedResult[0].Domain)
		}
		if expectedResult[0].Service != mdnsService {
			t.Fatalf("Expected service is %s, but got %s", mdnsService, expectedResult[0].Service)
		}
		if expectedResult[0].Instance != mdnsName {
			t.Fatalf("Expected instance is %s, but got %s", mdnsName, expectedResult[0].Instance)
		}
		if expectedResult[0].Port != mdnsPort {
			t.Fatalf("Expected port is %d, but got %d", mdnsPort, expectedResult[0].Port)
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
		entries := make(chan *ServiceEntry)
		var expectedResult []*ServiceEntry
		go func(results <-chan *ServiceEntry) {
			s := <-results
			expectedResult = append(expectedResult, s)
		}(entries)

		if err := resolver.Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
			t.Fatalf("Expected browse success, but got %v", err)
		}
		<-ctx.Done()

		if len(expectedResult) != 1 {
			t.Fatalf("Expected number of service entries is 1, but got %d", len(expectedResult))
		}
		if expectedResult[0].Domain != mdnsDomain {
			t.Fatalf("Expected domain is %s, but got %s", mdnsDomain, expectedResult[0].Domain)
		}
		if expectedResult[0].Service != mdnsService {
			t.Fatalf("Expected service is %s, but got %s", mdnsService, expectedResult[0].Service)
		}
		if expectedResult[0].Instance != mdnsName {
			t.Fatalf("Expected instance is %s, but got %s", mdnsName, expectedResult[0].Instance)
		}
		if expectedResult[0].Port != mdnsPort {
			t.Fatalf("Expected port is %d, but got %d", mdnsPort, expectedResult[0].Port)
		}
	})
}
