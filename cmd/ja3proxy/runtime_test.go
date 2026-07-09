package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newRuntimeTestApp(t *testing.T) *App {
	t.Helper()

	config := &RunningConfig{
		Addr:       "127.0.0.1",
		Port:       "0",
		TLSClient:  "Golang",
		TLSVersion: "0",
	}
	ca := &CertificateAuthority{}
	sessionKey := &SessionKeyHelper{}
	fingerprints := &TLSFingerprintStore{}

	return &App{
		Config:          config,
		CA:              ca,
		SessionKey:      sessionKey,
		TLSFingerprints: fingerprints,
	}
}

func TestEnsureCAReturnsErrorWhenOnlyCertExists(t *testing.T) {
	app := newRuntimeTestApp(t)
	dir := t.TempDir()
	app.Config.Cert = filepath.Join(dir, "cert.pem")
	app.Config.Key = filepath.Join(dir, "key.pem")

	if err := os.WriteFile(app.Config.Cert, []byte("cert"), 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	err := app.ensureCA()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "found CA cert") {
		t.Fatalf("error = %q, want CA cert context", err)
	}
}

func TestEnsureCAReturnsErrorWhenOnlyKeyExists(t *testing.T) {
	app := newRuntimeTestApp(t)
	dir := t.TempDir()
	app.Config.Cert = filepath.Join(dir, "cert.pem")
	app.Config.Key = filepath.Join(dir, "key.pem")

	if err := os.WriteFile(app.Config.Key, []byte("key"), 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	err := app.ensureCA()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "found CA key") {
		t.Fatalf("error = %q, want CA key context", err)
	}
}

func TestParseFlagsAppliesNormalizedArgs(t *testing.T) {
	app := newRuntimeTestApp(t)

	err := app.parseFlags([]string{
		"--ca-cert", "custom-cert.pem",
		"--ca-key", "custom-key.pem",
		"--listen", "127.0.0.1:9090",
		"--tls-fingerprint", "chrome@120",
		"--tls-profile-file", "upstream-tls.json",
		"--upstream-proxy", "socks5://127.0.0.1:1080",
		"--log-level", "warn",
	})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	if app.Config.Cert != "custom-cert.pem" {
		t.Fatalf("cert = %q, want custom-cert.pem", app.Config.Cert)
	}
	if app.Config.Key != "custom-key.pem" {
		t.Fatalf("key = %q, want custom-key.pem", app.Config.Key)
	}
	if app.Config.Listen != "127.0.0.1:9090" {
		t.Fatalf("listen = %q, want 127.0.0.1:9090", app.Config.Listen)
	}
	if app.Config.Addr != "127.0.0.1" {
		t.Fatalf("addr = %q, want 127.0.0.1", app.Config.Addr)
	}
	if app.Config.Port != "9090" {
		t.Fatalf("port = %q, want 9090", app.Config.Port)
	}
	if app.Config.TLSClient != "Chrome" {
		t.Fatalf("client = %q, want Chrome", app.Config.TLSClient)
	}
	if app.Config.TLSVersion != "120" {
		t.Fatalf("version = %q, want 120", app.Config.TLSVersion)
	}
	if app.Config.FingerprintConfig != "" {
		t.Fatalf("fingerprint config = %q, want empty", app.Config.FingerprintConfig)
	}
	if app.Config.UpstreamTLSConfig != "upstream-tls.json" {
		t.Fatalf("upstream TLS config = %q, want upstream-tls.json", app.Config.UpstreamTLSConfig)
	}
	if app.Config.Upstream != "socks5://127.0.0.1:1080" {
		t.Fatalf("upstream = %q, want socks5://127.0.0.1:1080", app.Config.Upstream)
	}
	if app.Config.LogLevel != "warn" {
		t.Fatalf("log level = %q, want warn", app.Config.LogLevel)
	}
	if app.Config.DumpTraffic {
		t.Fatal("dump traffic = true, want false")
	}
	if flag.CommandLine.Lookup("cert") != nil {
		t.Fatal("parseFlags registered cert on global flag.CommandLine")
	}
}

func TestParseFlagsAppliesTLSFingerprintFile(t *testing.T) {
	app := newRuntimeTestApp(t)

	err := app.parseFlags([]string{"--tls-fingerprint-file", "fingerprints.json"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if app.Config.FingerprintConfig != "fingerprints.json" {
		t.Fatalf("fingerprint config = %q, want fingerprints.json", app.Config.FingerprintConfig)
	}
}

func TestParseFlagsDumpTrafficImpliesDebugLogging(t *testing.T) {
	app := newRuntimeTestApp(t)

	err := app.parseFlags([]string{"--dump-traffic"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if !app.Config.DumpTraffic {
		t.Fatal("dump traffic = false, want true")
	}
	if app.Config.LogLevel != "debug" {
		t.Fatalf("log level = %q, want debug", app.Config.LogLevel)
	}
}

func TestParseFlagsReturnsErrorForInvalidFlag(t *testing.T) {
	app := newRuntimeTestApp(t)

	err := app.parseFlags([]string{"-unknown"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("error = %q, want unknown flag context", err)
	}
	if flag.CommandLine.Lookup("unknown") != nil {
		t.Fatal("parseFlags registered unknown on global flag.CommandLine")
	}
}

func TestParseFlagsAppliesFingerprintShorthand(t *testing.T) {
	app := newRuntimeTestApp(t)

	err := app.parseFlags([]string{"--tls-fingerprint", "chrome@120"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if app.Config.TLSClient != "Chrome" {
		t.Fatalf("client = %q, want Chrome", app.Config.TLSClient)
	}
	if app.Config.TLSVersion != "120" {
		t.Fatalf("version = %q, want 120", app.Config.TLSVersion)
	}
}

func TestParseFlagsReturnsErrorForInvalidFingerprintShorthand(t *testing.T) {
	app := newRuntimeTestApp(t)

	err := app.parseFlags([]string{"--tls-fingerprint", "chrome@999"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "available Chrome versions") {
		t.Fatalf("error = %q, want available versions", err)
	}
}

func TestParseFlagsRejectsRemovedFlag(t *testing.T) {
	app := newRuntimeTestApp(t)

	err := app.parseFlags([]string{"-port", "9090"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("error = %q, want unknown flag context", err)
	}
}

func TestParseFlagsRejectsFingerprintFileWithGlobalFingerprint(t *testing.T) {
	app := newRuntimeTestApp(t)

	err := app.parseFlags([]string{"--tls-fingerprint-file", "fingerprints.json", "--tls-fingerprint", "chrome@120"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--tls-fingerprint-file") {
		t.Fatalf("error = %q, want fingerprint file conflict context", err)
	}
}

func TestParseFlagsListFingerprintsSkipsFingerprintParsing(t *testing.T) {
	app := newRuntimeTestApp(t)

	err := app.parseFlags([]string{"--list-tls-fingerprints", "--tls-fingerprint", "not-a-browser"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if !app.Config.ListFingerprints {
		t.Fatal("list fingerprints = false, want true")
	}
}

func TestConfigureTLSFingerprintReturnsValidationError(t *testing.T) {
	app := newRuntimeTestApp(t)
	app.Config.TLSClient = "UnsupportedClient"
	app.Config.TLSVersion = "0"

	err := app.configureTLSFingerprint(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed configuring TLS fingerprint") {
		t.Fatalf("error = %q, want fingerprint context", err)
	}
}

func TestConfigureTLSFingerprintReturnsFileError(t *testing.T) {
	app := newRuntimeTestApp(t)
	app.Config.FingerprintConfig = filepath.Join(t.TempDir(), "missing.json")

	err := app.configureTLSFingerprint(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed loading fingerprint config") {
		t.Fatalf("error = %q, want fingerprint file context", err)
	}
}

func TestConfigureTLSFingerprintPassesContextToWatcher(t *testing.T) {
	type contextKey struct{}

	app := newRuntimeTestApp(t)
	app.Config.FingerprintConfig = filepath.Join(t.TempDir(), "fingerprint.json")

	baseCtx, cancel := context.WithCancel(context.Background())
	ctx := context.WithValue(baseCtx, contextKey{}, "runtime")
	called := false
	app.watchFingerprintFile = func(gotCtx context.Context, path string, interval time.Duration) error {
		called = true
		if gotCtx.Value(contextKey{}) != "runtime" {
			t.Fatal("watcher did not receive configured context")
		}
		if path != app.Config.FingerprintConfig {
			t.Fatalf("watch path = %q, want %q", path, app.Config.FingerprintConfig)
		}
		if interval != 2*time.Second {
			t.Fatalf("watch interval = %s, want 2s", interval)
		}

		cancel()
		select {
		case <-gotCtx.Done():
		default:
			t.Fatal("watcher context was not canceled")
		}
		return nil
	}

	if err := app.configureTLSFingerprint(ctx); err != nil {
		t.Fatalf("configureTLSFingerprint() error = %v", err)
	}
	if !called {
		t.Fatal("watcher was not called")
	}
}

func TestBuildProxyReturnsUpstreamValidationError(t *testing.T) {
	app := newRuntimeTestApp(t)
	app.Config.Upstream = "http://127.0.0.1:1080"

	proxy, err := app.buildProxy()
	if err == nil {
		t.Fatal("expected error")
	}
	if proxy != nil {
		t.Fatalf("proxy = %#v, want nil", proxy)
	}
	if !strings.Contains(err.Error(), "configure upstream proxy") {
		t.Fatalf("error = %q, want upstream context", err)
	}
}

func TestServeReturnsCanceledContext(t *testing.T) {
	app := newRuntimeTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := app.serve(ctx, NewProxy(nil, nil, nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("serve() error = %v, want context.Canceled", err)
	}
}
