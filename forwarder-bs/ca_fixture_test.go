package forwarderbs

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	caCertFile = "ca.crt"
	caKeyFile  = "ca.key"
	caValidity = 10 * 365 * 24 * time.Hour
)

func loadCAFromPaths(caCertPath, caKeyPath string) (*x509.Certificate, crypto.Signer, error) {
	data, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read CA cert: %w", err)
	}
	keyData, err := os.ReadFile(caKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read CA key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, nil, fmt.Errorf("decode CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA cert: %w", err)
	}
	keyBlock, _ := pem.Decode(keyData)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("decode CA key PEM")
	}
	caKey, err := parsePrivateKey(keyBlock)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA key: %w", err)
	}
	now := time.Now()
	if !caCert.IsCA || !caCert.BasicConstraintsValid || caCert.KeyUsage&x509.KeyUsageCertSign == 0 || now.Before(caCert.NotBefore) || now.After(caCert.NotAfter) {
		return nil, nil, fmt.Errorf("CA is not valid for certificate issuance")
	}
	public, err := x509.MarshalPKIXPublicKey(caKey.Public())
	if err != nil || !bytes.Equal(public, caCert.RawSubjectPublicKeyInfo) {
		return nil, nil, fmt.Errorf("CA certificate and private key do not match")
	}
	return caCert, caKey, nil
}

// parsePrivateKey returns a private key from a PEM block (crypto.Signer: ECDSA or RSA).
// It accepts EC PRIVATE KEY (SEC1), RSA PRIVATE KEY (PKCS#1), and PRIVATE KEY (PKCS#8, ECDSA or RSA).
func loadOrCreateCA(dir string) (*x509.Certificate, crypto.Signer, error) {
	caPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)
	_, certErr := os.Stat(caPath)
	_, keyErr := os.Stat(keyPath)
	if certErr == nil || keyErr == nil {
		return loadCAFromPaths(caPath, keyPath)
	}
	if !os.IsNotExist(certErr) || !os.IsNotExist(keyErr) {
		return nil, nil, fmt.Errorf("cannot inspect existing CA state")
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, _ := rand.Int(rand.Reader, maxSerial)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "kudzu-forwarder-bs-ca"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	caCert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	if err := writePEM(caPath, "CERTIFICATE", der); err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return nil, nil, err
	}
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER); err != nil {
		return nil, nil, err
	}

	return caCert, caKey, nil
}

func writePEM(path, typ string, data []byte) error {
	return atomicCredentialWrite(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: data}))
}
