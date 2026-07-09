package pipe

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestJunctionForwardsBothDirections(t *testing.T) {
	destConn, upstreamPeer := net.Pipe()
	clientConn, clientPeer := net.Pipe()
	conns := []net.Conn{destConn, upstreamPeer, clientConn, clientPeer}
	for _, conn := range conns {
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("set deadline: %v", err)
		}
	}

	done := make(chan struct{})
	go func() {
		junction(destConn, clientConn)
		close(done)
	}()

	assertForward := func(name string, src net.Conn, dst net.Conn, payload []byte) {
		t.Helper()

		writeErr := make(chan error, 1)
		go func() {
			_, err := src.Write(payload)
			writeErr <- err
		}()

		got := make([]byte, len(payload))
		if _, err := io.ReadFull(dst, got); err != nil {
			t.Fatalf("%s read: %v", name, err)
		}
		if string(got) != string(payload) {
			t.Fatalf("%s payload = %q, want %q", name, got, payload)
		}

		select {
		case err := <-writeErr:
			if err != nil {
				t.Fatalf("%s write: %v", name, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s write timed out", name)
		}
	}

	assertForward("client to upstream", clientPeer, upstreamPeer, []byte("client ping"))
	assertForward("upstream to client", upstreamPeer, clientPeer, []byte("upstream pong"))

	clientPeer.Close()
	upstreamPeer.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("junction did not return after peer connections closed")
	}
}

func TestJunctionReturnsWhenOneSideCloses(t *testing.T) {
	destConn, upstreamPeer := net.Pipe()
	clientConn, clientPeer := net.Pipe()
	conns := []net.Conn{destConn, upstreamPeer, clientConn, clientPeer}
	for _, conn := range conns {
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("set deadline: %v", err)
		}
	}

	done := make(chan struct{})
	go func() {
		junction(destConn, clientConn)
		close(done)
	}()

	if err := clientPeer.Close(); err != nil {
		t.Fatalf("clientPeer.Close() error = %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("junction did not return after one side closed")
	}
}
