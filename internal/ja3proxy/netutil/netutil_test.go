package netutil

import "testing"

func TestStripPort(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "host port", in: "example.com:443", want: "example.com"},
		{name: "bare host", in: "example.com", want: "example.com"},
		{name: "ipv4 port", in: "127.0.0.1:443", want: "127.0.0.1"},
		{name: "ipv6 brackets", in: "[2001:db8::1]", want: "2001:db8::1"},
		{name: "ipv6 port", in: "[2001:db8::1]:443", want: "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripPort(tt.in); got != tt.want {
				t.Fatalf("StripPort(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
