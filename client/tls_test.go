package client

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type testCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}
	return &testCA{
		cert:    cert,
		key:     key,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

func (ca *testCA) issueServer(t *testing.T, names ...string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test-server"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     names,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create server cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal server key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("load server key pair: %v", err)
	}
	return tlsCert
}

func writeTempPEM(t *testing.T, pemBytes []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}
	return path
}

func TestLoadRootCAsSystemOnly(t *testing.T) {
	pool, err := loadRootCAs("")
	if err != nil {
		t.Fatalf("loadRootCAs: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil system cert pool")
	}
}

func TestLoadRootCAsAppendsCustomCA(t *testing.T) {
	ca := newTestCA(t)
	caFile := writeTempPEM(t, ca.certPEM)

	pool, err := loadRootCAs(caFile)
	if err != nil {
		t.Fatalf("loadRootCAs: %v", err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{ca.issueServer(t, "localhost")}}
	server.StartTLS()
	t.Cleanup(server.Close)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("GET with custom CA: %v", err)
	}
	resp.Body.Close()
}

func TestLoadRootCAsRejectsMissingFile(t *testing.T) {
	_, err := loadRootCAs(filepath.Join(t.TempDir(), "missing.pem"))
	if err == nil {
		t.Fatal("expected error for missing CA file")
	}
}

func TestLoadRootCAsRejectsMalformedPEM(t *testing.T) {
	caFile := writeTempPEM(t, []byte("not a certificate\n"))
	_, err := loadRootCAs(caFile)
	if err == nil {
		t.Fatal("expected error for malformed CA PEM")
	}
}

func TestLoadTLSConfigUsesSystemRootsByDefault(t *testing.T) {
	cfg, err := loadTLSConfig("", nil)
	if err != nil {
		t.Fatalf("loadTLSConfig: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Fatal("expected system RootCAs")
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("unexpected MinVersion: %v", cfg.MinVersion)
	}
}
