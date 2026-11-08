package main

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeGatewayID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"01-02-03-04-05-06-07-08", "0102030405060708"},
		{"01:02:03:04:05:06:07:08", "0102030405060708"},
		{"0102030405060708", "0102030405060708"},
		{"AA-BB-CC-DD-EE-FF-00-11", "aabbccddeeff0011"},
		{"x-y-z", ""}, // non-hex chars become _
	}
	for _, tt := range tests {
		got := NormalizeGatewayID(tt.in)
		if got != tt.want {
			t.Errorf("NormalizeGatewayID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCertStore_GetOrCreateClientCert_emptyIDRejected(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCertStore(dir, "", "")
	if err != nil {
		t.Fatalf("NewCertStore: %v", err)
	}
	_, err = store.GetOrCreateClientCert("")
	if err == nil {
		t.Fatal("GetOrCreateClientCert(\"\") should return an error")
	}
}

func TestCertStore_GetOrCreateClientCert(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCertStore(dir, "", "")
	if err != nil {
		t.Fatalf("NewCertStore: %v", err)
	}
	gatewayID := "0102030405060708"
	cert, err := store.GetOrCreateClientCert(gatewayID)
	if err != nil {
		t.Fatalf("GetOrCreateClientCert: %v", err)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatal("expected non-empty certificate")
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	if parsed.Subject.CommonName != gatewayID {
		t.Errorf("CN = %q, want %q", parsed.Subject.CommonName, gatewayID)
	}
	if len(parsed.ExtKeyUsage) == 0 || parsed.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Errorf("ExtKeyUsage = %v, want ClientAuth", parsed.ExtKeyUsage)
	}
	// Persistence: create a new store with same dir, should load the same cert
	store2, err := NewCertStore(dir, "", "")
	if err != nil {
		t.Fatalf("NewCertStore (reload): %v", err)
	}
	cert2, err := store2.GetOrCreateClientCert(gatewayID)
	if err != nil {
		t.Fatalf("GetOrCreateClientCert (reload): %v", err)
	}
	parsed2, _ := x509.ParseCertificate(cert2.Certificate[0])
	if parsed2.SerialNumber.Cmp(parsed.SerialNumber) != 0 {
		t.Errorf("reloaded cert has different serial; persistence failed")
	}
}

func TestCertStore_GetOrCreateClientCert_chainsToCA(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCertStore(dir, "", "")
	if err != nil {
		t.Fatalf("NewCertStore: %v", err)
	}
	cert, err := store.GetOrCreateClientCert("gw1")
	if err != nil {
		t.Fatalf("GetOrCreateClientCert: %v", err)
	}
	// Load CA from disk and verify the client cert chains to it
	caPEM, err := os.ReadFile(filepath.Join(dir, caCertFile))
	if err != nil {
		t.Fatalf("read CA cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	_, err = parsed.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		t.Errorf("client cert does not verify against CA: %v", err)
	}
}

func TestCertStore_GetOrCreateBootstrap(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCertStore(dir, "", "")
	if err != nil {
		t.Fatalf("NewCertStore: %v", err)
	}
	boot, err := store.GetOrCreateBootstrap()
	if err != nil {
		t.Fatalf("GetOrCreateBootstrap: %v", err)
	}
	if boot == nil || len(boot.Certificate) == 0 {
		t.Fatal("expected non-empty bootstrap certificate")
	}
	parsed, err := x509.ParseCertificate(boot.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	if parsed.Subject.CommonName != "kudzu-forwarder-bs-bootstrap" {
		t.Errorf("CN = %q", parsed.Subject.CommonName)
	}
	// Second call returns same cert (from disk)
	boot2, err := store.GetOrCreateBootstrap()
	if err != nil {
		t.Fatalf("GetOrCreateBootstrap (second): %v", err)
	}
	parsed2, err := x509.ParseCertificate(boot2.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate (bootstrap2): %v", err)
	}
	if parsed.SerialNumber.Cmp(parsed2.SerialNumber) != 0 {
		t.Error("second bootstrap cert should be same as first")
	}
}
