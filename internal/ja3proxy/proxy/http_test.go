package proxy

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/lylemi/ja3proxy/internal/ja3proxy/traffic"
)

func TestHTTPProxyAuthentication(t *testing.T) {
	called := false
	proxy := NewProxy(nil, nil, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		if got := req.Header.Get("Proxy-Authorization"); got != "" {
			t.Fatalf("Proxy-Authorization leaked upstream: %q", got)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
	})).WithAuthentication("client", "secret")

	request := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	token := base64.StdEncoding.EncodeToString([]byte("client:secret"))
	request.Header.Set("Proxy-Authorization", "Basic "+token)
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if !called {
		t.Fatal("authenticated request was not forwarded")
	}
}

func TestHTTPProxyAuthenticationRequired(t *testing.T) {
	proxy := NewProxy(nil, nil, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("unauthenticated request must not be forwarded")
		return nil, nil
	})).WithAuthentication("client", "secret")

	request := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)

	if response.Code != http.StatusProxyAuthRequired {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusProxyAuthRequired)
	}
	if got := response.Header().Get("Proxy-Authenticate"); got != proxyAuthRealm {
		t.Fatalf("Proxy-Authenticate = %q, want %q", got, proxyAuthRealm)
	}
}

func TestCopyHeader(t *testing.T) {
	src := http.Header{}
	src.Add("Set-Cookie", "a=1")
	src.Add("Set-Cookie", "b=2")
	src.Set("Content-Type", "text/plain")

	dst := http.Header{}
	copyHeader(dst, src)

	if got := dst.Values("Set-Cookie"); !reflect.DeepEqual(got, []string{"a=1", "b=2"}) {
		t.Fatalf("copied Set-Cookie values = %v, want [a=1 b=2]", got)
	}
	if got := dst.Get("Content-Type"); got != "text/plain" {
		t.Fatalf("copied Content-Type = %q, want text/plain", got)
	}
}

func TestHandleHTTPRecordsTraffic(t *testing.T) {
	monitor := traffic.NewTrafficMonitor()
	proxy := NewProxy(nil, nil, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if string(body) != "request" {
			t.Fatalf("request body = %q, want request", body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("response")),
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Request:    req,
		}, nil
	})).WithTrafficMonitor(monitor)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/resource", strings.NewReader("request"))
	req.RemoteAddr = "127.0.0.1:50000"
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "response" {
		t.Fatalf("response body = %q, want response", rec.Body.String())
	}

	snapshot := monitor.Snapshot()
	if snapshot.TotalUploadBytes != int64(len("request")) {
		t.Fatalf("upload bytes = %d, want %d", snapshot.TotalUploadBytes, len("request"))
	}
	if snapshot.TotalDownloadBytes != int64(len("response")) {
		t.Fatalf("download bytes = %d, want %d", snapshot.TotalDownloadBytes, len("response"))
	}
	if len(snapshot.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(snapshot.Sessions))
	}
	if snapshot.Sessions[0].State != traffic.StateClosed {
		t.Fatalf("session state = %q, want closed", snapshot.Sessions[0].State)
	}
	if snapshot.Sessions[0].Target != "example.com" {
		t.Fatalf("session target = %q, want example.com", snapshot.Sessions[0].Target)
	}
}

func TestHandleHTTPWritesUpstreamResponse(t *testing.T) {
	var upstreamReq *http.Request
	proxy := NewProxy(nil, nil, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		upstreamReq = req
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header: http.Header{
				"Content-Type": {"text/plain"},
				"Set-Cookie":   {"a=1", "b=2"},
				"X-Test":       {"ok"},
			},
			Body: io.NopCloser(strings.NewReader("proxied body")),
		}, nil
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/resource", nil)
	req.RequestURI = "http://example.com/resource"
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", got)
	}
	if got := resp.Header.Values("Set-Cookie"); !reflect.DeepEqual(got, []string{"a=1", "b=2"}) {
		t.Fatalf("Set-Cookie values = %v, want [a=1 b=2]", got)
	}
	if got := resp.Header.Get("X-Test"); got != "ok" {
		t.Fatalf("X-Test = %q, want ok", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if got := string(body); got != "proxied body" {
		t.Fatalf("body = %q, want proxied body", got)
	}
	if upstreamReq == nil {
		t.Fatal("RoundTrip was not called")
	}
	if upstreamReq.RequestURI != "" {
		t.Fatalf("upstream RequestURI = %q, want empty", upstreamReq.RequestURI)
	}
}

func TestHandleHTTPRoundTripErrorReturnsServiceUnavailable(t *testing.T) {
	proxy := NewProxy(nil, nil, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("upstream unavailable")
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/resource", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if !strings.Contains(string(body), "upstream unavailable") {
		t.Fatalf("body = %q, want upstream error", string(body))
	}
}
