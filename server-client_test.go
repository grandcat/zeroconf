package zeroconf

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grandcat/zeroconf/client"
	"github.com/grandcat/zeroconf/connection"
	"github.com/grandcat/zeroconf/server"
	"github.com/grandcat/zeroconf/utils"
	"github.com/miekg/dns"
)

func TestSrvClt(t *testing.T) {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.With(logger, "ts", log.TimestampFormat(func() time.Time { return time.Now().UTC() }, "Jan 02 15:04:05.99"), "caller", log.DefaultCaller)

	ctx := context.Background()

	clt, err := client.New(ctx, logger,
		client.Config{
			IPProtocol: connection.IPv6,
		},
	)
	utils.ExitOnError(logger, err, "create client")

	defer func() {
		clt.Stop()
	}()
	go func() {
		clt.Run()
		<-ctx.Done()
	}()

	srv, err := server.New(ctx, logger,
		server.Config{
			IPProtocol: server.IPv6,
		},
	)
	utils.ExitOnError(logger, err, "create server")

	var services []*connection.Service

	expSrvcs := make(map[string]connection.Service)
	actSrvcs := make(map[string]connection.Service)

	service, err := connection.NewService("c", "_controller1._tcp", "", "", 11111, []string{"T1"})
	utils.ExitOnError(logger, err, "create service")
	services = append(services, service)
	expSrvcs[service.InstanceNameFQDN()] = *service

	service, err = connection.NewService("c", "_controller2._tcp", "", "", 22222, []string{"T2"})
	utils.ExitOnError(logger, err, "create service")
	services = append(services, service)
	expSrvcs[service.InstanceNameFQDN()] = *service

	service, err = connection.NewService("c", "_controller3._tcp", "", "", 33333, []string{"T3"})
	utils.ExitOnError(logger, err, "create service")
	services = append(services, service)
	expSrvcs[service.InstanceNameFQDN()] = *service

	go func() {
		srv.Run()
		<-ctx.Done()
	}()

	utils.ExitOnError(logger, srv.Register(services), "register services")
	defer func() {
		srv.Stop()
	}()

	q := []dns.Question{{Name: connection.DNSSDQuestion + "local.", Qtype: dns.TypePTR, Qclass: dns.ClassINET}}
	err = clt.Query(q)
	if err != nil {
		level.Error(logger).Log("msg", "sending client request", "err", err)
	}

	ticker := time.NewTicker(1 * time.Second)
MainLoop:
	for {
		select {
		case <-ticker.C:
			t.Fatal("time to receive a service expired")
		case s := <-clt.Receive():
			actSrvcs[s.InstanceNameFQDN()] = s
			if len(actSrvcs) == 3 {
				break MainLoop
			}
		}
	}
	utils.Equals(t, expSrvcs, actSrvcs)

	clt.SetFilter("T1")
	actSrvcs = make(map[string]connection.Service)

MainLoop1:
	for {
		ticker = time.NewTicker(1 * time.Second)
		select {
		case <-ticker.C:
			t.Fatal("time to receive a service expired")
		case s := <-clt.Receive():
			actSrvcs[s.NameFQDN()] = s
			if len(actSrvcs) == 1 {
				break MainLoop1
			}
		}
	}

	utils.Equals(t, expSrvcs, actSrvcs)
}
