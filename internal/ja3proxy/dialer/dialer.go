package dialer

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

type UpstreamDialer struct {
	dialer    proxy.Dialer
	Transport http.RoundTripper
}

// DynamicUpstreamDialer lets new connections pick up an upstream proxy change
// without interrupting connections that are already in flight.
type DynamicUpstreamDialer struct {
	mu       sync.RWMutex
	current  *UpstreamDialer
	upstream string
	timeout  time.Duration
}

func NewDynamicUpstreamDialer(socksAddr string, timeout time.Duration) (*DynamicUpstreamDialer, error) {
	socksAddr = strings.TrimSpace(socksAddr)
	current, err := NewUpstreamDialer(socksAddr, timeout)
	if err != nil {
		return nil, err
	}
	return &DynamicUpstreamDialer{current: current, upstream: socksAddr, timeout: timeout}, nil
}

func (u *DynamicUpstreamDialer) Configure(socksAddr string) error {
	socksAddr = strings.TrimSpace(socksAddr)
	next, err := NewUpstreamDialer(socksAddr, u.timeout)
	if err != nil {
		return err
	}

	u.mu.Lock()
	previous := u.current
	u.current = next
	u.upstream = socksAddr
	u.mu.Unlock()

	if previous != nil {
		if transport, ok := previous.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
	return nil
}

func (u *DynamicUpstreamDialer) Upstream() string {
	if u == nil {
		return ""
	}
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.upstream
}

func (u *DynamicUpstreamDialer) Dial(network, addr string) (net.Conn, error) {
	u.mu.RLock()
	current := u.current
	u.mu.RUnlock()
	return current.Dial(network, addr)
}

func (u *DynamicUpstreamDialer) RoundTrip(request *http.Request) (*http.Response, error) {
	u.mu.RLock()
	current := u.current
	u.mu.RUnlock()
	return current.Transport.RoundTrip(request)
}

func NewUpstreamDialer(socksAddr string, timeout time.Duration) (*UpstreamDialer, error) {
	var dialer proxy.Dialer
	var transport http.RoundTripper

	if socksAddr != "" {
		parsedURL, err := parseSocksURL(socksAddr)
		if err != nil {
			return nil, err
		}
		user := parsedURL.User.Username()
		password, _ := parsedURL.User.Password()
		socksDialer, err := proxy.SOCKS5(
			"tcp", parsedURL.Host,
			&proxy.Auth{User: user, Password: password},
			proxy.Direct,
		)
		if err != nil {
			return nil, err
		}
		dialer = socksDialer

		defaultTransport := http.DefaultTransport.(*http.Transport).Clone()
		defaultTransport.Proxy = func(req *http.Request) (*url.URL, error) {
			return parsedURL, nil
		}
		transport = defaultTransport
	} else {
		directDialer := &net.Dialer{Timeout: timeout}
		dialer = directDialer
		directTransport := http.DefaultTransport.(*http.Transport).Clone()
		directTransport.Proxy = nil
		directTransport.DialContext = directDialer.DialContext
		transport = directTransport
	}

	return &UpstreamDialer{
		dialer:    dialer,
		Transport: transport,
	}, nil
}

func parseSocksURL(socksAddr string) (*url.URL, error) {
	parsedURL, err := url.Parse(socksAddr)
	if err != nil || !strings.Contains(socksAddr, "://") {
		parsedURL, err = url.Parse("socks5://" + socksAddr)
		if err != nil {
			return nil, err
		}
	}
	if parsedURL.Scheme == "" {
		parsedURL.Scheme = "socks5"
	}
	if parsedURL.Scheme != "socks5" {
		return nil, fmt.Errorf("unsupported upstream proxy scheme %q", parsedURL.Scheme)
	}
	if parsedURL.Host == "" {
		return nil, fmt.Errorf("missing upstream proxy host")
	}
	return parsedURL, nil
}

func (u *UpstreamDialer) Dial(network, addr string) (net.Conn, error) {
	return u.dialer.Dial(network, addr)
}
