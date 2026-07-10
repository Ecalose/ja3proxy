package ja3proxy

import (
	"net"
	"testing"
	"time"
)

func TestRebindableListenerAcceptsFromReplacementSocket(t *testing.T) {
	initial, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on initial socket: %v", err)
	}
	listener := newRebindableListener(initial)
	t.Cleanup(func() { _ = listener.Close() })

	accepted := make(chan net.Conn, 1)
	errs := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			errs <- acceptErr
			return
		}
		accepted <- conn
	}()

	replacement, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on replacement socket: %v", err)
	}
	if err := listener.Rebind(replacement); err != nil {
		t.Fatalf("Rebind() error = %v", err)
	}

	client, err := net.DialTimeout("tcp", replacement.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("dial replacement socket: %v", err)
	}
	defer client.Close()

	select {
	case conn := <-accepted:
		_ = conn.Close()
	case err := <-errs:
		t.Fatalf("Accept() error = %v", err)
	case <-time.After(time.Second):
		t.Fatal("replacement socket did not accept a connection")
	}

	if _, err := net.DialTimeout("tcp", initial.Addr().String(), 100*time.Millisecond); err == nil {
		t.Fatal("initial socket still accepts connections after rebind")
	}
}
