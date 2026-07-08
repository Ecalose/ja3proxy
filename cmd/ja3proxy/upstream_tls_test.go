package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUpstreamTLSConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstream-tls.json")
	if err := os.WriteFile(path, []byte(`{
		"default": {"protocol": "utls", "client": "Chrome", "version": "120"},
		"routes": [
			{"host": "*.example.com", "protocol": "utls", "client": "Firefox", "version": "105"}
		]
	}`), 0o600); err != nil {
		t.Fatalf("write upstream TLS config: %v", err)
	}

	got, err := loadUpstreamTLSConfigFile(path)
	if err != nil {
		t.Fatalf("loadUpstreamTLSConfigFile() error = %v", err)
	}
	if got.Default.Protocol != "utls" || got.Default.Client != "Chrome" || got.Default.Version != "120" {
		t.Fatalf("default profile = %+v, want utls Chrome 120", got.Default)
	}
	if len(got.Routes) != 1 {
		t.Fatalf("route count = %d, want 1", len(got.Routes))
	}
	if got.Routes[0].Host != "*.example.com" || got.Routes[0].Protocol != "utls" ||
		got.Routes[0].Client != "Firefox" || got.Routes[0].Version != "105" {
		t.Fatalf("route = %+v, want *.example.com utls Firefox 105", got.Routes[0])
	}
}

func TestUpstreamTLSProfileStoreMatchesRoutes(t *testing.T) {
	store := &UpstreamTLSProfileStore{}
	if err := store.SetValidated(UpstreamTLSConfig{
		Default: UpstreamTLSProfile{Protocol: "utls", Client: "Chrome", Version: "120"},
		Routes: []UpstreamTLSRoute{
			{
				Host:               "*.example.com",
				UpstreamTLSProfile: UpstreamTLSProfile{Protocol: "utls", Client: "Firefox", Version: "105"},
			},
			{
				Host:               "api.example.com",
				UpstreamTLSProfile: UpstreamTLSProfile{Protocol: "utls", Client: "360Browser", Version: "7.5"},
			},
		},
	}); err != nil {
		t.Fatalf("SetValidated() error = %v", err)
	}

	tests := []struct {
		host       string
		wantClient string
		wantVer    string
	}{
		{host: "api.example.com", wantClient: "360Browser", wantVer: "7.5"},
		{host: "www.example.com", wantClient: "Firefox", wantVer: "105"},
		{host: "WWW.EXAMPLE.COM:443", wantClient: "Firefox", wantVer: "105"},
		{host: "example.com", wantClient: "Chrome", wantVer: "120"},
		{host: "other.test", wantClient: "Chrome", wantVer: "120"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got, ok := store.Get(tt.host)
			if !ok {
				t.Fatal("Get() ok = false, want profile")
			}
			if got.Protocol != upstreamTLSProtocolUTLS || got.Client != tt.wantClient || got.Version != tt.wantVer {
				t.Fatalf("Get(%q) = %+v, want %s %s", tt.host, got, tt.wantClient, tt.wantVer)
			}
		})
	}
}

func TestValidateUpstreamTLSConfigRejectsInvalidProfiles(t *testing.T) {
	tests := []struct {
		name   string
		config UpstreamTLSConfig
		want   string
	}{
		{
			name: "unsupported protocol",
			config: UpstreamTLSConfig{
				Default: UpstreamTLSProfile{Protocol: "not-a-protocol", Client: "Chrome", Version: "120"},
			},
			want: "unsupported upstream TLS protocol",
		},
		{
			name: "missing utls client",
			config: UpstreamTLSConfig{
				Default: UpstreamTLSProfile{Protocol: "utls", Version: "120"},
			},
			want: "utls client is required",
		},
		{
			name: "missing utls version",
			config: UpstreamTLSConfig{
				Default: UpstreamTLSProfile{Protocol: "utls", Client: "Chrome"},
			},
			want: "utls version is required",
		},
		{
			name: "route missing utls client",
			config: UpstreamTLSConfig{
				Default: UpstreamTLSProfile{Protocol: "utls", Client: "Chrome", Version: "120"},
				Routes: []UpstreamTLSRoute{
					{
						Host:               "*.example.com",
						UpstreamTLSProfile: UpstreamTLSProfile{Protocol: "utls", Version: "105"},
					},
				},
			},
			want: "utls client is required",
		},
		{
			name: "route missing utls version",
			config: UpstreamTLSConfig{
				Default: UpstreamTLSProfile{Protocol: "utls", Client: "Chrome", Version: "120"},
				Routes: []UpstreamTLSRoute{
					{
						Host:               "*.example.com",
						UpstreamTLSProfile: UpstreamTLSProfile{Protocol: "utls", Client: "Firefox"},
					},
				},
			},
			want: "utls version is required",
		},
		{
			name: "missing route host",
			config: UpstreamTLSConfig{
				Routes: []UpstreamTLSRoute{
					{UpstreamTLSProfile: UpstreamTLSProfile{Protocol: "utls", Client: "Chrome", Version: "120"}},
				},
			},
			want: "host is required",
		},
		{
			name: "bad wildcard",
			config: UpstreamTLSConfig{
				Routes: []UpstreamTLSRoute{
					{
						Host:               "api.*.example.com",
						UpstreamTLSProfile: UpstreamTLSProfile{Protocol: "utls", Client: "Chrome", Version: "120"},
					},
				},
			},
			want: "wildcard host must start",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUpstreamTLSConfig(tt.config)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateUpstreamTLSConfig() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestConfiguredUpstreamTLSProfileFallsBackToCurrentFingerprint(t *testing.T) {
	store := &TLSFingerprintStore{}
	store.Set(TLSFingerprint{Client: "Firefox", Version: "105"})
	handler := &TunnelHandler{
		TLSFingerprints:   store,
		DefaultTLSClient:  "Chrome",
		DefaultTLSVersion: "120",
	}

	got := handler.configuredUpstreamTLSProfile("example.com")
	if got.Protocol != upstreamTLSProtocolUTLS || got.Client != "Firefox" || got.Version != "105" {
		t.Fatalf("configuredUpstreamTLSProfile() = %+v, want Firefox 105", got)
	}
}

func TestConfiguredUpstreamTLSProfileUsesRouteStore(t *testing.T) {
	profiles := &UpstreamTLSProfileStore{}
	profiles.Set(UpstreamTLSConfig{
		Default: UpstreamTLSProfile{Protocol: "utls", Client: "Chrome", Version: "120"},
		Routes: []UpstreamTLSRoute{
			{
				Host:               "*.example.com",
				UpstreamTLSProfile: UpstreamTLSProfile{Protocol: "utls", Client: "Firefox", Version: "105"},
			},
		},
	})
	handler := &TunnelHandler{
		UpstreamTLSProfiles: profiles,
		DefaultTLSClient:    "Golang",
		DefaultTLSVersion:   "0",
	}

	got := handler.configuredUpstreamTLSProfile("api.example.com")
	if got.Protocol != upstreamTLSProtocolUTLS || got.Client != "Firefox" || got.Version != "105" {
		t.Fatalf("configuredUpstreamTLSProfile() = %+v, want Firefox 105 route", got)
	}
}
