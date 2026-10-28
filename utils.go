package zeroconf

import (
	"strings"

	"github.com/miekg/dns"
)

func parseSubtypes(service string) (string, []string) {
	subtypes := strings.Split(service, ",")
	return subtypes[0], subtypes[1:]
}

// trimDot is used to trim the dots from the start or end of a string
func trimDot(s string) string {
	return strings.Trim(s, ".")
}

// packMulticastMessage serializes a message for multicast transmission.
func packMulticastMessage(msg *dns.Msg) ([]byte, error) {
	// RFC 6762 section 18.1 requires zero IDs for multicast responses and
	// recommends them for queries. Preserve the original for legacy unicast.
	multicast := *msg
	multicast.Id = 0
	return multicast.Pack()
}
