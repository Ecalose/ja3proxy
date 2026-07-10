package proxy

import (
	"io"
	"net"
	"testing"
)

func writeSOCKS5Greeting(t *testing.T, conn net.Conn, method byte) {
	t.Helper()
	if _, err := conn.Write([]byte{socks5Version, 0x01, method}); err != nil {
		t.Fatalf("write SOCKS5 greeting: %v", err)
	}
}

func writeSOCKS5UserPassAuth(t *testing.T, conn net.Conn, username, password string) {
	t.Helper()
	request := []byte{socks5AuthVersion, byte(len(username))}
	request = append(request, username...)
	request = append(request, byte(len(password)))
	request = append(request, password...)
	if _, err := conn.Write(request); err != nil {
		t.Fatalf("write SOCKS5 username/password authentication: %v", err)
	}
}

func writeSOCKS5ConnectRequest(t *testing.T, conn net.Conn, host string, port uint16) {
	t.Helper()
	request := socks5RequestBytes(socks5Version, socks5Connect, socks5Reserved, socks5Domain, []byte(host), port)
	if _, err := conn.Write(request); err != nil {
		t.Fatalf("write SOCKS5 connect request: %v", err)
	}
}

func writeSOCKS5Request(t *testing.T, conn net.Conn, command byte, rsv byte, atyp byte, address []byte, port uint16) {
	t.Helper()
	if _, err := conn.Write(socks5RequestBytes(socks5Version, command, rsv, atyp, address, port)); err != nil {
		t.Fatalf("write SOCKS5 request: %v", err)
	}
}

func socks5RequestBytes(version byte, command byte, rsv byte, atyp byte, address []byte, port uint16) []byte {
	request := []byte{version, command, rsv, atyp}
	if atyp == socks5Domain {
		request = append(request, byte(len(address)))
	}
	request = append(request, address...)
	request = append(request, byte(port>>8), byte(port))
	return request
}

func readExact(t *testing.T, conn net.Conn, want []byte) {
	t.Helper()
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read bytes: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("read bytes = %v, want %v", got, want)
	}
}
