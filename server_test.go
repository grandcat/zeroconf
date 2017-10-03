package zeroconf

import (
	"testing"
)

func TestRaceShutdown(t *testing.T) {
	var name, service, domain = "GoZeroconfGo", "_workstation._tcp", "local."
	var port = 42424
	server, err := Register(name, service, domain, port, []string{"txtv=0", "lo=1", "la=2"}, nil)
	if err != nil {
		panic(err)
	}
	if err = server.Shutdown(); err != nil {
		t.Fatal(err)
	}
}
