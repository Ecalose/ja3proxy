<p align="center">
  <img src="assets/logo.svg" alt="JA3Proxy logo" width="520">
</p>

# JA3Proxy

JA3Proxy is an HTTP/SOCKS5 proxy that uses
[uTLS](https://github.com/refraction-networking/utls) to create outbound TLS
connections with configurable ClientHello fingerprints. It can be used to test
how applications behave behind different browser-like TLS fingerprints, while
keeping familiar proxy interfaces for clients.

## Features

- HTTP, HTTPS, and SOCKS5 proxy support on the same listen address.
- Customizable TLS ClientHello fingerprints through uTLS presets.
- Dynamic MITM certificates for HTTPS `CONNECT` traffic.
- Automatic local CA generation when no certificate/key pair is provided.
- Optional SOCKS5 upstream proxy for both HTTP and HTTPS traffic.
- Docker and Docker Compose examples included.

## How it works

For plain HTTP requests, JA3Proxy forwards the request directly. For HTTPS
`CONNECT` requests, it establishes a TLS connection to the upstream server using
the configured uTLS fingerprint, then serves a dynamically generated certificate
to the client using the local CA. SOCKS5 connections are accepted on the same
listen address: TLS streams use the same MITM/uTLS path, while non-TLS streams
are forwarded as plain TCP.

Because HTTPS traffic is intercepted, clients must either trust the generated CA
certificate or explicitly skip certificate verification for testing.

## Quick start

### Build from source

Requirements:

- Go 1.24 or newer
- `make` if you want to use the provided Makefile

```bash
git clone https://github.com/lylemi/ja3proxy.git
cd ja3proxy

go build -o ja3proxy ./cmd/ja3proxy
./ja3proxy --listen :8080 --tls-fingerprint 360Browser@7.5
```

The CLI source lives in `cmd/ja3proxy`.

Test the proxy:

```bash
curl -v -k --proxy http://127.0.0.1:8080 https://www.example.com
curl -v -k --proxy socks5h://127.0.0.1:8080 https://www.example.com
```

The first run creates `credentials/cert.pem` and `credentials/key.pem` if they
do not already exist.

### Docker

```bash
mkdir -p credentials

docker run --rm \
  -v ./credentials:/app/credentials \
  -p 8080:8080 \
  ghcr.io/lylemi/ja3proxy:latest \
  --ca-cert /app/credentials/cert.pem \
  --ca-key /app/credentials/key.pem \
  --tls-fingerprint 360Browser@7.5
```

### Docker Compose

```bash
docker compose up -d
```

See [compose.yaml](./compose.yaml) for the full service definition.

## Configuration

```text
Usage:
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
```

Example with a SOCKS5 upstream proxy:

```bash
./ja3proxy \
  --listen :8080 \
  --tls-fingerprint chrome@106 \
  --upstream-proxy socks5://127.0.0.1:1080
```

The `--upstream-proxy` flag also accepts `host:port`, for example
`127.0.0.1:1080`. Only SOCKS5 upstream proxies are supported.

Global TLS fingerprint sources are mutually exclusive: use one of
`--tls-fingerprint` or `--tls-fingerprint-file`.

### Upstream TLS profile config

Use `--tls-profile-file` when different upstream hosts need different outbound
TLS fingerprints. The flag loads a JSON file with a default upstream TLS profile
and optional host-specific routes:

```json
{
  "default": {
    "protocol": "utls",
    "client": "Chrome",
    "version": "120"
  },
  "routes": [
    {
      "host": "*.gm.example.com",
      "protocol": "utls",
      "client": "360Browser",
      "version": "7.5"
    },
    {
      "host": "api.example.com",
      "protocol": "utls",
      "client": "Firefox",
      "version": "105"
    }
  ]
}
```

Start the proxy with the profile file:

```bash
./ja3proxy --listen :8080 --tls-profile-file upstream-tls.json
```

JA3Proxy uses the `default` profile when no route matches. If `default` is
omitted, unmatched hosts continue to use the global fingerprint from
`--tls-fingerprint` or `--tls-fingerprint-file`.

Each profile currently supports `protocol: "utls"` with the same `client` and
`version` values described in [TLS fingerprints](#tls-fingerprints). `tlcp` is
intentionally not implemented yet, so `tlcp` profiles are not supported in this
version.

Route `host` values match the upstream destination host. Matching supports exact
hosts such as `api.example.com` and leading wildcard patterns such as
`*.example.com`.

This differs from `--tls-fingerprint-file`: `--tls-fingerprint-file`
hot-reloads one global uTLS `client`/`version` pair for all upstream hosts,
while `--tls-profile-file` selects a profile by upstream host from the JSON
file.

### Hot-reload TLS fingerprints

Use `--tls-fingerprint-file` to load the uTLS fingerprint from a JSON file and
watch it for changes:

```json
{
  "client": "Chrome",
  "version": "106"
}
```

Start the proxy with the file:

```bash
./ja3proxy --listen :8080 --tls-fingerprint-file fingerprint.json
```

When the file changes, JA3Proxy validates and reloads it. New HTTPS `CONNECT`
connections use the latest fingerprint; existing TLS tunnels keep the
fingerprint they were opened with. If a reload fails, the previous fingerprint
stays active and the error is logged.

## TLS fingerprints

JA3Proxy resolves global fingerprint settings and upstream TLS profiles to uTLS
ClientHello presets. The easiest global setting is `--tls-fingerprint`:

```bash
./ja3proxy --tls-fingerprint chrome@120
./ja3proxy --tls-fingerprint firefox
```

The `client@version` form selects an exact preset. If the version is omitted,
JA3Proxy uses the default version from the current uTLS dependency.

To see the presets supported by the binary you built, run:

```bash
./ja3proxy --list-tls-fingerprints
```

Current presets in this module:

| Client | Versions | Default |
| --- | --- | --- |
| Golang | 0 | 0 |
| Chrome | 58, 62, 70, 72, 83, 87, 96, 100, 100_PSK, 102, 106, 112_PSK, 114_PSK, 115_PQ, 115_PQ_PSK, 120, 120_PQ, 131, 133 | 133 |
| Firefox | 55, 56, 63, 65, 99, 102, 105, 120 | 120 |
| iOS | 111, 12.1, 13, 14 | 14 |
| Android | 11 | 11 |
| Edge | 85, 106 | 85 |
| Safari | 16.0 | 16.0 |
| 360Browser | 7.5, 11.0 | 7.5 |
| QQBrowser | 11.1 | 11.1 |

The `--tls-fingerprint` shorthand is case-insensitive for client names and
accepts `auto`, `default`, or `latest` as the default version. For iOS,
`ios@11.1` is accepted as an alias for uTLS's `111` version value.

With `--tls-profile-file`, each matched profile supplies the same `client`
and `version` values. Supported presets depend on the uTLS version used by this
project. See the uTLS
[ClientHelloID definitions](https://github.com/refraction-networking/utls/blob/master/u_common.go)
for the upstream definitions.

## Updating uTLS

The uTLS library is compiled into the JA3Proxy binary, so updating it requires a
rebuild. Dependabot is configured to open weekly pull requests for Go module
updates, including `github.com/refraction-networking/utls`. Those pull requests
run the Go CI workflow before they are merged.

To update manually:

```bash
go get github.com/refraction-networking/utls@latest
go mod tidy
go test ./...
```

## Certificates

JA3Proxy needs a CA certificate and private key to generate per-host
certificates for HTTPS interception.

- If both files exist, they are loaded from `--ca-cert` and `--ca-key`.
- If neither file exists, JA3Proxy generates a new CA pair.
- If only one file exists, startup fails to avoid using a mismatched pair.

By default, generated CA files are written to `credentials/cert.pem` and
`credentials/key.pem`. If the configured paths include missing directories,
JA3Proxy creates them before writing the files.

For browser or application testing, import the generated CA certificate into the
client trust store. For one-off command-line checks, tools such as `curl -k`
can skip verification.

## Development

Run the test suite:

```bash
go test ./...
```

Build release binaries with the Makefile:

```bash
make
```

This creates Linux and Windows AMD64 binaries in the `bin/` directory.

## Security notice

JA3Proxy performs TLS interception and can expose decrypted traffic to the
machine running the proxy. Use it only in environments where you have permission
to inspect the traffic. Protect generated CA private keys carefully and remove
them from client trust stores when they are no longer needed.

## Contributing

Issues and pull requests are welcome. Please include a clear description,
reproduction steps when reporting bugs, and tests for behavior changes when
practical.

## License

This project is licensed under the [MIT License](./LICENSE).
