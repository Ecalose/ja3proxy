package e2e

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func newProxyHTTPClient(t *testing.T, proxyServerURL string, tlsConfig *tls.Config) *http.Client {
	t.Helper()

	proxyURL, err := url.Parse(proxyServerURL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	transport := &http.Transport{
		Proxy:               http.ProxyURL(proxyURL),
		TLSClientConfig:     tlsConfig,
		ForceAttemptHTTP2:   false,
		TLSHandshakeTimeout: 2 * time.Second,
	}
	t.Cleanup(transport.CloseIdleConnections)

	return &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}
}
