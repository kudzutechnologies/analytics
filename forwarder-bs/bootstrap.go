package forwarderbs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrIssuanceDisabled = errors.New("certificate issuance is disabled")

type GatewayBootstrap struct {
	EUI              string
	Trust, Cert, Key []byte
	Pin              string
}

func material(b []byte, path string) ([]byte, error) {
	if len(b) > 0 {
		return b, nil
	}
	if path == "" {
		return nil, nil
	}
	return os.ReadFile(path)
}
func IssueGatewayBootstrap(c ForwarderConfig, eui string) (GatewayBootstrap, error) {
	id, e := ParseGatewayID(eui)
	if e != nil {
		return GatewayBootstrap{}, e
	}
	trust, e := material(c.CertsCACertPEM, c.CertsCACert)
	if e != nil {
		return GatewayBootstrap{}, e
	}
	cert, e := material(c.IssuerCertPEM, c.IssuerCert)
	if e != nil {
		return GatewayBootstrap{}, e
	}
	key, e := material(c.IssuerKeyPEM, c.IssuerKey)
	if e != nil {
		return GatewayBootstrap{}, e
	}
	if len(cert) == 0 && len(key) == 0 {
		cert = trust
		key, e = material(c.CertsCAKeyPEM, c.CertsCAKey)
		if e != nil {
			return GatewayBootstrap{}, e
		}
	}
	if len(key) == 0 {
		return GatewayBootstrap{}, ErrIssuanceDisabled
	}
	block, rest := pem.Decode(cert)
	if block == nil {
		return GatewayBootstrap{}, fmt.Errorf("invalid issuer certificate")
	}
	issuer, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		return GatewayBootstrap{}, e
	}
	kb, _ := pem.Decode(key)
	if kb == nil {
		return GatewayBootstrap{}, fmt.Errorf("invalid issuer key")
	}
	signer, e := parsePrivateKey(kb)
	if e != nil {
		return GatewayBootstrap{}, e
	}
	pub, e := x509.MarshalPKIXPublicKey(signer.Public())
	if e != nil || string(pub) != string(issuer.RawSubjectPublicKeyInfo) || !issuer.IsCA || issuer.KeyUsage&x509.KeyUsageCertSign == 0 {
		return GatewayBootstrap{}, fmt.Errorf("invalid issuer signing pair")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(trust) {
		return GatewayBootstrap{}, fmt.Errorf("invalid CA PEM")
	}
	inter := x509.NewCertPool()
	inter.AppendCertsFromPEM(rest)
	if _, e = issuer.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); e != nil {
		return GatewayBootstrap{}, e
	}
	private, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return GatewayBootstrap{}, e
	}
	serial, e := rand.Int(rand.Reader, maxSerial)
	if e != nil {
		return GatewayBootstrap{}, e
	}
	until := time.Now().Add(certValidity)
	if issuer.NotAfter.Before(until) {
		until = issuer.NotAfter
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: id}, NotBefore: time.Now(), NotAfter: until, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, leaf, issuer, &private.PublicKey, signer)
	if e != nil {
		return GatewayBootstrap{}, e
	}
	kd, e := x509.MarshalECPrivateKey(private)
	if e != nil {
		return GatewayBootstrap{}, e
	}
	cp := pemCertificate(der)
	if string(issuer.RawIssuer) != string(issuer.RawSubject) {
		cp = append(cp, cert...)
	}
	digest := sha256.Sum256(der)
	return GatewayBootstrap{id, trust, cp, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), hex.EncodeToString(digest[:])}, nil
}
func ExportGatewayBootstrap(c ForwarderConfig) error {
	b, e := IssueGatewayBootstrap(c, c.BootstrapGW)
	if e != nil {
		return e
	}
	dir := c.BootstrapDir
	if dir == "" {
		dir = "bootstrap"
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	dir = filepath.Join(dir, b.EUI)
	if e = os.Mkdir(dir, 0700); e != nil {
		return e
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(dir)
		}
	}()
	for name, data := range map[string][]byte{"cups.trust": b.Trust, "cups.crt": b.Cert, "cups.key": b.Key} {
		if e = os.WriteFile(filepath.Join(dir, name), data, 0600); e != nil {
			return e
		}
	}
	ok = true
	fmt.Printf("Bootstrap: %s\nSHA256: %s\n", dir, b.Pin)
	return nil
}
