package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/lylemi/ja3proxy/internal/ja3proxy/certstore"
	"github.com/lylemi/ja3proxy/internal/ja3proxy/tunnel"
	utls "github.com/refraction-networking/utls"
)

func localTLSCertificate(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test TLS key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"upstream.test"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create test TLS certificate: %v", err)
	}
	keyDER := x509.MarshalPKCS1PrivateKey(key)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("parse test TLS certificate: %v", err)
	}
	return cert
}

func newTestTunnelHandler(t *testing.T, clientHelloID utls.ClientHelloID) *tunnel.TunnelHandler {
	t.Helper()

	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca-key.pem")

	ca := &certstore.CertificateAuthority{}
	if err := ca.Generate(certPath, keyPath); err != nil {
		t.Fatalf("CertificateAuthority.Generate() error = %v", err)
	}
	session := &certstore.SessionKeyHelper{}
	if err := session.Generate(); err != nil {
		t.Fatalf("SessionKeyHelper.Generate() error = %v", err)
	}

	return &tunnel.TunnelHandler{
		CA:                ca,
		SessionKey:        session,
		DefaultTLSClient:  clientHelloID.Client,
		DefaultTLSVersion: clientHelloID.Version,
	}
}
