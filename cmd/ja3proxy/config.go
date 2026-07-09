package main

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
)

type RunningConfig struct {
	DumpTraffic       bool
	LogLevel          string
	Listen            string
	Addr              string
	Port              string
	TLSVersion        string
	TLSClient         string
	ListFingerprints  bool
	FingerprintConfig string
	UpstreamTLSConfig string
	Cert              string
	Key               string
	Upstream          string
	TUI               bool
}

type CertificateAuthority struct {
	tlsCert  tls.Certificate
	x509Cert *x509.Certificate
}

type SessionKeyHelper struct {
	privateKey *ecdsa.PrivateKey
	PEMBlock   []byte
}
