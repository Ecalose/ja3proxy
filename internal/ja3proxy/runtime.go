package ja3proxy

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
	"github.com/lylemi/ja3proxy/internal/ja3proxy/certstore"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/dialer"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/fingerprint"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/logutil"
	httpproxy "github.com/lylemi/ja3proxy/internal/ja3proxy/proxy"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/traffic"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/tui"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/tunnel"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/upstreamtls"
)

type App struct {
	Config              *RunningConfig
	CA                  *certstore.CertificateAuthority
	SessionKey          *certstore.SessionKeyHelper
	TLSFingerprints     *fingerprint.TLSFingerprintStore
	UpstreamTLSProfiles *upstreamtls.UpstreamTLSProfileStore
	TrafficMonitor      *traffic.TrafficMonitor

	watchFingerprintFile func(context.Context, string, time.Duration) error
}

// Run executes JA3Proxy using the current process arguments.
func Run() error {
	return newDefaultApp().run()
}

func newDefaultApp() *App {
	return &App{
		Config:              &RunningConfig{},
		CA:                  &certstore.CertificateAuthority{},
		SessionKey:          &certstore.SessionKeyHelper{},
		TLSFingerprints:     &fingerprint.TLSFingerprintStore{},
		UpstreamTLSProfiles: &upstreamtls.UpstreamTLSProfileStore{},
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
		fmt.Print(fingerprint.FormatCatalog())
		return nil
	}

	if err := app.configureRuntime(ctx); err != nil {
		return err
	}
	proxyServer, err := app.buildProxy()
	if err != nil {
		return err
	}
	return app.serveConfiguredProxy(ctx, proxyServer)
}

func (app *App) configureRuntime(ctx context.Context) error {
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
	app.ensureTrafficMonitor()
	return nil
}

func (app *App) ensureTrafficMonitor() {
	if app.Config.TUI && app.TrafficMonitor == nil {
		app.TrafficMonitor = traffic.NewTrafficMonitor()
	}
}

func (app *App) serveConfiguredProxy(ctx context.Context, proxyServer *httpproxy.Proxy) error {
	if app.Config.TUI {
		return app.serveWithTUI(ctx, proxyServer)
	}
	return app.serve(ctx, proxyServer)
}

func (app *App) serveWithTUI(ctx context.Context, proxyServer *httpproxy.Proxy) error {
	return tui.Run(ctx, tui.Config{
		ListenAddress: app.Config.listenAddress(),
		TLSClient:     app.Config.TLSClient,
		TLSVersion:    app.Config.TLSVersion,
	}, app.TrafficMonitor, func(runCtx context.Context) error {
		return app.serve(runCtx, proxyServer)
	})
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

		logutil.Info(
			"runtime",
			"generating missing CA certificate and key",
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
	} else if err := app.TLSFingerprints.SetValidated(fingerprint.TLSFingerprint{
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
		app.UpstreamTLSProfiles = &upstreamtls.UpstreamTLSProfileStore{}
	}
	if err := app.UpstreamTLSProfiles.ApplyFile(app.Config.UpstreamTLSConfig); err != nil {
		return fmt.Errorf("failed loading upstream TLS config: %w", err)
	}
	return nil
}

func (app *App) buildProxy() (*httpproxy.Proxy, error) {
	upstreamDialer, err := dialer.NewUpstreamDialer(app.Config.Upstream, time.Second*10)
	if err != nil {
		return nil, fmt.Errorf("configure upstream proxy: %w", err)
	}

	proxyServer := httpproxy.NewProxy(upstreamDialer.Dial, app.tunnelHandler().Connect, upstreamDialer.Transport)
	if app.TrafficMonitor != nil {
		proxyServer.WithTrafficMonitor(app.TrafficMonitor)
	}
	return proxyServer, nil
}

func (app *App) serve(ctx context.Context, proxyServer *httpproxy.Proxy) error {
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
		Handler: proxyServer,
	}

	fingerprint := app.configuredTLSFingerprint()
	logutil.Info(
		"runtime",
		"proxy server listening",
		"protocols", "HTTP/SOCKS5",
		"addr", listenAddress,
		"tls_client", fingerprint.Client,
		"tls_version", fingerprint.Version,
	)
	stopClosingServer := context.AfterFunc(ctx, func() {
		_ = server.Close()
	})
	defer stopClosingServer()

	if err := server.Serve(httpproxy.NewMixedProxyListener(listener, proxyServer)); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && (errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed)) {
			return ctxErr
		}
		return fmt.Errorf("serve proxy: %w", err)
	}
	return nil
}

func (app *App) configuredTLSFingerprint() fingerprint.TLSFingerprint {
	if app.TLSFingerprints != nil {
		if fp, ok := app.TLSFingerprints.Get(); ok {
			return fp
		}
	}

	return fingerprint.TLSFingerprint{
		Client:  app.Config.TLSClient,
		Version: app.Config.TLSVersion,
	}
}

func (app *App) tunnelHandler() *tunnel.TunnelHandler {
	return &tunnel.TunnelHandler{
		Debug:               app.Config.dumpTrafficEnabled(),
		CA:                  app.CA,
		SessionKey:          app.SessionKey,
		TLSFingerprints:     app.TLSFingerprints,
		UpstreamTLSProfiles: app.UpstreamTLSProfiles,
		DefaultTLSClient:    app.Config.TLSClient,
		DefaultTLSVersion:   app.Config.TLSVersion,
	}
}
