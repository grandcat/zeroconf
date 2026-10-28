package zeroconf

import (
	"testing"

	"github.com/miekg/dns"
)

func TestPackMulticastMessage(t *testing.T) {
	for _, response := range []bool{false, true} {
		name := "query"
		if response {
			name = "response"
		}
		t.Run(name, func(t *testing.T) {
			msg := &dns.Msg{}
			msg.SetQuestion("_http._tcp.local.", dns.TypePTR)
			// Both a query made with SetQuestion and a response made with
			// SetReply may have a nonzero ID before multicast serialization.
			msg.Id = 0x1234
			msg.Response = response
			msg.RecursionDesired = false
			if response {
				msg.Answer = []dns.RR{&dns.PTR{
					Hdr: dns.RR_Header{Name: "_http._tcp.local.", Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 120},
					Ptr: "Example._http._tcp.local.",
				}}
			}

			packet, err := packMulticastMessage(msg)
			if err != nil {
				t.Fatal(err)
			}
			var decoded dns.Msg
			if err := decoded.Unpack(packet); err != nil {
				t.Fatal(err)
			}
			if decoded.Id != 0 {
				t.Errorf("multicast wire ID = %d, want 0", decoded.Id)
			}
			if decoded.Response != response || decoded.RecursionDesired || len(decoded.Question) != 1 || decoded.Question[0] != msg.Question[0] || len(decoded.Answer) != len(msg.Answer) {
				t.Error("serialization changed the message content")
			}
			if response && decoded.Answer[0].String() != msg.Answer[0].String() {
				t.Error("serialization changed the response answer")
			}
			if msg.Id != 0x1234 {
				t.Error("serialization changed the original ID needed by legacy unicast responses")
			}
		})
	}
}
