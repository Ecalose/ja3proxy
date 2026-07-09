package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	cflog "github.com/cloudflare/cfssl/log"
)

type App struct {
	Config              *RunningConfig
	CA                  *CertificateAuthority
	SessionKey          *SessionKeyHelper
	TLSFingerprints     *TLSFingerprintStore
	UpstreamTLSProfiles *UpstreamTLSProfileStore

	watchFingerprintFile func(context.Context, string, time.Duration) error
}

func newDefaultApp() *App {
	return &App{
		Config:              &RunningConfig{},
		CA:                  &CertificateAuthority{},
		SessionKey:          &SessionKeyHelper{},
		TLSFingerprints:     &TLSFingerprintStore{},
		UpstreamTLSProfiles: &UpstreamTLSProfileStore{},
	}
}

func (app *App) run() error {
	return app.runWithContext(context.Background())
}

func (app *App) runWithContext(ctx context.Context) error {
	ctx = runtimeContext(ctx)

	if err := app.parseFlags(os.Args[1:]); err != nil {
		return err
	}
	if app.Config.ListFingerprints {
		fmt.Print(formatTLSFingerprintCatalog())
		return nil
	}
	app.configureLogging()
	if err := app.ensureCA(); err != nil {
		return err
	}
	if err := app.loadExistingCA(); err != nil {
		return fmt.Errorf("failed loading CA: %w", err)
	}
	if err := app.generateSessionKey(); err != nil {
		return fmt.Errorf("failed generating session key: %w", err)
	}
	if err := app.configureTLSFingerprint(ctx); err != nil {
		return err
	}
	if err := app.configureUpstreamTLSProfiles(); err != nil {
		return err
	}

	proxy, err := app.buildProxy()
	if err != nil {
		return err
	}
	return app.serve(ctx, proxy)
}

func runtimeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (app *App) configureLogging() {
	level := logLevelFromName(app.Config.LogLevel)
	if app.Config.dumpTrafficEnabled() && level > slog.LevelDebug {
		level = slog.LevelDebug
	}
	cflog.Level = cflog.LevelWarning
	if level <= slog.LevelDebug {
		cflog.Level = cflog.LevelDebug
	}

	configureDefaultLogger(level)
}

func (app *App) ensureCA() error {
	if !fileExists(app.Config.Cert) || !fileExists(app.Config.Key) {
		if fileExists(app.Config.Cert) {
			return fmt.Errorf("found CA cert %q, but no corresponding key %q", app.Config.Cert, app.Config.Key)
		} else if fileExists(app.Config.Key) {
			return fmt.Errorf("found CA key %q, but no corresponding cert %q", app.Config.Key, app.Config.Cert)
		}

		slog.Info(
			"generating missing CA certificate and key",
			"component", "runtime",
			"cert", app.Config.Cert,
			"key", app.Config.Key,
		)
		if err := app.CA.Generate(app.Config.Cert, app.Config.Key); err != nil {
			return fmt.Errorf("failed generating CA: %w", err)
		}
	}
	return nil
}

func (app *App) loadExistingCA() error {
	return app.CA.Load(app.Config.Cert, app.Config.Key)
}

func (app *App) generateSessionKey() error {
	return app.SessionKey.Generate()
}

func (app *App) configureTLSFingerprint(ctx context.Context) error {
	if app.Config.FingerprintConfig != "" {
		if err := app.watchTLSFingerprintFile(runtimeContext(ctx), app.Config.FingerprintConfig, 2*time.Second); err != nil {
			return fmt.Errorf("failed loading fingerprint config: %w", err)
		}
	} else if err := app.TLSFingerprints.SetValidated(TLSFingerprint{
		Client:  app.Config.TLSClient,
		Version: app.Config.TLSVersion,
	}); err != nil {
		return fmt.Errorf("failed configuring TLS fingerprint: %w", err)
	}
	return nil
}

func (app *App) watchTLSFingerprintFile(ctx context.Context, path string, interval time.Duration) error {
	if app.watchFingerprintFile != nil {
		return app.watchFingerprintFile(ctx, path, interval)
	}
	return app.TLSFingerprints.WatchFile(ctx, path, interval)
}

func (app *App) configureUpstreamTLSProfiles() error {
	if app.Config.UpstreamTLSConfig == "" {
		return nil
	}
	if app.UpstreamTLSProfiles == nil {
		app.UpstreamTLSProfiles = &UpstreamTLSProfileStore{}
	}
	if err := app.UpstreamTLSProfiles.ApplyFile(app.Config.UpstreamTLSConfig); err != nil {
		return fmt.Errorf("failed loading upstream TLS config: %w", err)
	}
	return nil
}

func (app *App) buildProxy() (*Proxy, error) {
	dialer, err := NewUpstreamDialer(app.Config.Upstream, time.Second*10)
	if err != nil {
		return nil, fmt.Errorf("configure upstream proxy: %w", err)
	}

	return NewProxy(dialer.Dial, app.tunnelHandler().Connect, dialer.Transport), nil
}

func (app *App) serve(ctx context.Context, proxy *Proxy) error {
	ctx = runtimeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	listenAddress := app.Config.listenAddress()
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listenAddress, err)
	}
	server := &http.Server{
		Handler: proxy,
	}

	fingerprint := app.configuredTLSFingerprint()
	slog.Info(
		"proxy server listening",
		"component", "runtime",
		"protocols", "HTTP/SOCKS5",
		"addr", listenAddress,
		"tls_client", fingerprint.Client,
		"tls_version", fingerprint.Version,
	)
	stopClosingServer := context.AfterFunc(ctx, func() {
		_ = server.Close()
	})
	defer stopClosingServer()

	if err := server.Serve(newMixedProxyListener(listener, proxy)); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && (errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed)) {
			return ctxErr
		}
		return fmt.Errorf("serve proxy: %w", err)
	}
	return nil
}

func (app *App) configuredTLSFingerprint() TLSFingerprint {
	if fingerprint, ok := app.TLSFingerprints.Get(); ok {
		return fingerprint
	}

	return TLSFingerprint{
		Client:  app.Config.TLSClient,
		Version: app.Config.TLSVersion,
	}
}

func (app *App) tunnelHandler() *TunnelHandler {
	return &TunnelHandler{
		Debug:               app.Config.dumpTrafficEnabled(),
		CA:                  app.CA,
		SessionKey:          app.SessionKey,
		TLSFingerprints:     app.TLSFingerprints,
		UpstreamTLSProfiles: app.UpstreamTLSProfiles,
		DefaultTLSClient:    app.Config.TLSClient,
		DefaultTLSVersion:   app.Config.TLSVersion,
	}
}
