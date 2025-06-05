package zeroconf

import (
	"reflect"
	"testing"
)

func TestNewServiceRecordSubtypes(t *testing.T) {
	tests := []struct {
		name         string
		serviceInput string
		domain       string
		expected     []string
	}{
		{
			name:         "single subtype",
			serviceInput: "_http._tcp,_printer",
			domain:       "local",
			expected:     []string{"_printer._sub._http._tcp.local."},
		},
		{
			name:         "multiple subtypes",
			serviceInput: "_ftp._tcp,_secure,_fast",
			domain:       "local",
			expected: []string{
				"_secure._sub._ftp._tcp.local.",
				"_fast._sub._ftp._tcp.local.",
			},
		},
		{
			name:         "no subtypes",
			serviceInput: "_ssh._tcp",
			domain:       "local",
			expected:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := NewServiceRecord("TestInstance", tt.serviceInput, tt.domain)
			if !reflect.DeepEqual(sr.Subtypes, tt.expected) {
				t.Errorf("Subtypes = %v; want %v", sr.Subtypes, tt.expected)
			}
		})
	}
}
