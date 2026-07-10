package ja3proxy

import (
	"net"
	"sync"
)

// rebindableListener lets the proxy accept new connections on a replacement
// socket without closing connections accepted by the previous socket.
type rebindableListener struct {
	mu      sync.RWMutex
	current net.Listener
	closed  bool
}

func newRebindableListener(listener net.Listener) *rebindableListener {
	return &rebindableListener{current: listener}
}

func (listener *rebindableListener) Accept() (net.Conn, error) {
	for {
		listener.mu.RLock()
		current := listener.current
		closed := listener.closed
		listener.mu.RUnlock()
		if closed {
			return nil, net.ErrClosed
		}

		conn, err := current.Accept()
		if err == nil {
			return conn, nil
		}

		listener.mu.RLock()
		changed := current != listener.current
		closed = listener.closed
		listener.mu.RUnlock()
		if !changed || closed {
			return nil, err
		}
	}
}

func (listener *rebindableListener) Addr() net.Addr {
	listener.mu.RLock()
	defer listener.mu.RUnlock()
	return listener.current.Addr()
}

func (listener *rebindableListener) Rebind(next net.Listener) error {
	listener.mu.Lock()
	if listener.closed {
		listener.mu.Unlock()
		return net.ErrClosed
	}
	previous := listener.current
	listener.current = next
	listener.mu.Unlock()
	// The replacement is already live at this point and cannot be rolled back
	// safely. A close error on the retired socket must not invalidate it.
	_ = previous.Close()
	return nil
}

func (listener *rebindableListener) Close() error {
	listener.mu.Lock()
	if listener.closed {
		listener.mu.Unlock()
		return nil
	}
	listener.closed = true
	current := listener.current
	listener.mu.Unlock()
	return current.Close()
}
