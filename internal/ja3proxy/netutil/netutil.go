package netutil

import (
	"net"
	"strings"
)

// StripPort removes a host:port suffix while preserving bare hostnames.
// Credit: elazarl/goproxy (https://github.com/elazarl/goproxy/blob/7cc037d33fb57d20c2fa7075adaf0e2d2862da78/https.go#L50)
func StripPort(s string) string {
	host, _, err := net.SplitHostPort(s)
	if err == nil {
		return host
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	}
	return s
}

func RemoteAddr(conn net.Conn) string {
	if conn == nil || conn.RemoteAddr() == nil {
		return ""
	}
	return conn.RemoteAddr().String()
}
