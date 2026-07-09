package ja3proxy

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/lylemi/ja3proxy/internal/ja3proxy/fingerprint"
)

const (
	defaultListen       = ":8080"
	defaultCACertPath   = "credentials/cert.pem"
	defaultCAKeyPath    = "credentials/key.pem"
	defaultTLSClient    = "Golang"
	defaultTLSVersion   = "0"
	defaultLogLevelName = "info"
)

type cliOptions struct {
	listen              string
	caCert              string
	caKey               string
	tlsFingerprint      string
	tlsFingerprintFile  string
	tlsProfileFile      string
	upstreamProxy       string
	logLevel            string
	dumpTraffic         bool
	tui                 bool
	listTLSFingerprints bool
}

func (app *App) parseFlags(args []string) error {
	if app.Config == nil {
		app.Config = &RunningConfig{}
	}

	options := newDefaultCLIOptions()
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.Usage = func() {
		writeCLIUsage(flags.Output())
	}
	registerCLIFlags(flags, &options)

	if err := flags.Parse(args); err != nil {
		return err
	}
	return applyCLIOptions(app.Config, options, visitedFlagNames(flags))
}

func newDefaultCLIOptions() cliOptions {
	return cliOptions{
		listen:   defaultListen,
		caCert:   defaultCACertPath,
		caKey:    defaultCAKeyPath,
		logLevel: defaultLogLevelName,
	}
}

func registerCLIFlags(flags *flag.FlagSet, options *cliOptions) {
	flags.StringVar(&options.listen, "listen", defaultListen, "listen address, e.g. :8080 or 127.0.0.1:8080")

	flags.StringVar(&options.caCert, "ca-cert", defaultCACertPath, "proxy CA certificate path")
	flags.StringVar(&options.caKey, "ca-key", defaultCAKeyPath, "proxy CA private key path")

	flags.StringVar(&options.tlsFingerprint, "tls-fingerprint", "", "global uTLS fingerprint, e.g. chrome@120")
	flags.StringVar(&options.tlsFingerprintFile, "tls-fingerprint-file", "", "JSON file to hot-reload the global uTLS fingerprint")
	flags.StringVar(&options.tlsProfileFile, "tls-profile-file", "", "JSON file with host-routed upstream TLS profiles")
	flags.BoolVar(&options.listTLSFingerprints, "list-tls-fingerprints", false, "list supported uTLS fingerprints and exit")

	flags.StringVar(&options.upstreamProxy, "upstream-proxy", "", "upstream SOCKS5 proxy, e.g. socks5://127.0.0.1:1080")

	flags.StringVar(&options.logLevel, "log-level", defaultLogLevelName, "log level: debug, info, warn, error")
	flags.BoolVar(&options.dumpTraffic, "dump-traffic", false, "log proxied payload data; sensitive; implies debug logging")
	flags.BoolVar(&options.tui, "tui", false, "show a live terminal traffic dashboard")
}

func writeCLIUsage(output io.Writer) {
	fmt.Fprint(output, `Usage:
  ja3proxy [options]

Server:
  --listen string                 listen address, e.g. :8080 or 127.0.0.1:8080 (default ":8080")

CA:
  --ca-cert string                proxy CA certificate path (default "credentials/cert.pem")
  --ca-key string                 proxy CA private key path (default "credentials/key.pem")

TLS fingerprint:
  --tls-fingerprint string        global uTLS fingerprint, e.g. chrome@120
  --tls-fingerprint-file string   JSON file to hot-reload the global uTLS fingerprint
  --tls-profile-file string       JSON file with host-routed upstream TLS profiles
  --list-tls-fingerprints         list supported uTLS fingerprints and exit

Proxy:
  --upstream-proxy string         upstream SOCKS5 proxy, e.g. socks5://127.0.0.1:1080

Diagnostics:
  --log-level string              log level: debug, info, warn, error (default "info")
  --dump-traffic                  log proxied payload data; sensitive; implies debug logging
  --tui                           show a live terminal traffic dashboard
`)
}

func visitedFlagNames(flags *flag.FlagSet) map[string]bool {
	visited := make(map[string]bool)
	flags.Visit(func(flag *flag.Flag) {
		visited[flag.Name] = true
	})
	return visited
}

func applyCLIOptions(config *RunningConfig, options cliOptions, specified map[string]bool) error {
	config.ListFingerprints = options.listTLSFingerprints
	if config.ListFingerprints {
		return nil
	}

	listen, addr, port, err := resolveListenOption(options)
	if err != nil {
		return err
	}
	config.Listen = listen
	config.Addr = addr
	config.Port = port

	config.Cert = options.caCert
	config.Key = options.caKey
	config.FingerprintConfig = options.tlsFingerprintFile
	config.UpstreamTLSConfig = options.tlsProfileFile
	config.Upstream = options.upstreamProxy
	config.TUI = options.tui

	if err := applyTLSFingerprintOptions(config, options, specified); err != nil {
		return err
	}
	if err := applyDiagnosticOptions(config, options, specified); err != nil {
		return err
	}
	return nil
}

func resolveListenOption(options cliOptions) (listen string, addr string, port string, err error) {
	return normalizeListenAddress(options.listen)
}

func normalizeListenAddress(address string) (listen string, addr string, port string, err error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", "", "", fmt.Errorf("listen address is required")
	}
	if !strings.Contains(address, ":") {
		if _, parseErr := strconv.Atoi(address); parseErr != nil {
			return "", "", "", fmt.Errorf("listen address must include a port")
		}
		address = ":" + address
	}

	host, listenPort, err := net.SplitHostPort(address)
	if err != nil {
		return "", "", "", err
	}
	if listenPort == "" {
		return "", "", "", fmt.Errorf("listen port is required")
	}
	portNumber, err := strconv.Atoi(listenPort)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", "", fmt.Errorf("listen port must be a number from 1 to 65535")
	}
	return net.JoinHostPort(host, listenPort), host, listenPort, nil
}

func applyTLSFingerprintOptions(config *RunningConfig, options cliOptions, specified map[string]bool) error {
	fingerprintSpec := ""
	hasFingerprintSpec := false
	if specified["tls-fingerprint"] {
		fingerprintSpec = options.tlsFingerprint
		hasFingerprintSpec = true
	}

	if config.FingerprintConfig != "" && hasFingerprintSpec {
		return fmt.Errorf("use either --tls-fingerprint-file or global TLS fingerprint flags, not both")
	}

	config.TLSClient = defaultTLSClient
	config.TLSVersion = defaultTLSVersion
	if !hasFingerprintSpec {
		return nil
	}

	fp, err := fingerprint.ParseSpec(fingerprintSpec)
	if err != nil {
		return err
	}
	config.TLSClient = fp.Client
	config.TLSVersion = fp.Version
	return nil
}

func applyDiagnosticOptions(config *RunningConfig, options cliOptions, specified map[string]bool) error {
	config.DumpTraffic = options.dumpTraffic

	logLevel := options.logLevel
	if !specified["log-level"] && config.DumpTraffic {
		logLevel = "debug"
	}
	normalized, err := normalizeLogLevelName(logLevel)
	if err != nil {
		return err
	}
	if config.DumpTraffic {
		normalized = "debug"
	}
	config.LogLevel = normalized
	return nil
}

func normalizeLogLevelName(logLevel string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(logLevel)) {
	case "", "info":
		return "info", nil
	case "debug":
		return "debug", nil
	case "warn", "warning":
		return "warn", nil
	case "error":
		return "error", nil
	default:
		return "", fmt.Errorf("unsupported log level %q", logLevel)
	}
}

func logLevelFromName(logLevel string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(logLevel)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (config *RunningConfig) listenAddress() string {
	if config == nil {
		return defaultListen
	}
	if config.Listen != "" {
		return config.Listen
	}
	if config.Port == "" {
		return defaultListen
	}
	return net.JoinHostPort(config.Addr, config.Port)
}

func (config *RunningConfig) dumpTrafficEnabled() bool {
	return config != nil && config.DumpTraffic
}
