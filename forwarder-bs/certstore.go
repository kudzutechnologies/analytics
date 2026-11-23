package forwarderbs

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	caCertFile    = "ca.crt"
	caKeyFile     = "ca.key"
	bootstrapCert = "bootstrap.crt"
	bootstrapKey  = "bootstrap.key"
	certValidity  = 365 * 24 * time.Hour
	caValidity    = 10 * 365 * 24 * time.Hour
)

// CertStore holds per-gateway client certs, the CA, per-gateway LNS config (from router-info), and legacy data URIs.
// certs = our issued per-gateway certs (keyed by stable gateway ID, e.g. normalized router or client cert CN);
// serverCerts = certs issued by upstream CUPS; lnsConfigs = LNS-allocated config per gateway; dataURIs = legacy per-gateway URI (superseded by lnsConfig when set).
type CertStore struct {
	operationContext context.Context
	Persistence      GatewayPersistence
	IssuerConfig     *ForwarderConfig
	writable         bool
	dir              string
	caCert           *x509.Certificate
	caKey            crypto.Signer
	mu               sync.RWMutex
	certs            map[string]*tls.Certificate
	serverCerts      map[string]*tls.Certificate
	lnsConfigs       map[string]*GatewayLNSConfig
	dataURIs         map[string]string
}

// NewCertStore explicitly enables issuance and loads or creates its CA.
func NewCertStore(dir, caCertPath, caKeyPath string) (*CertStore, error) {
	return openCertStore(dir, caCertPath, caKeyPath, true)
}

// OpenCredentialStore reads imported keys without creating a CA or writing files.
func OpenCredentialStore(dir string) (*CertStore, error) { return openCertStore(dir, "", "", false) }
func openCertStore(dir, caCertPath, caKeyPath string, writable bool) (*CertStore, error) {
	if (caCertPath == "") != (caKeyPath == "") {
		return nil, fmt.Errorf("CA certificate and key must be configured together")
	}
	if writable {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("certs-dir: %w", err)
		}
	}
	var caCert *x509.Certificate
	var caKey crypto.Signer
	var err error
	if !writable {
	} else if caCertPath != "" && caKeyPath != "" {
		caCert, caKey, err = loadCAFromPaths(caCertPath, caKeyPath)
	} else {
		caCert, caKey, err = loadCAFromPaths(filepath.Join(dir, caCertFile), filepath.Join(dir, caKeyFile))
	}
	if err != nil {
		return nil, err
	}
	s := &CertStore{
		writable:    writable,
		dir:         dir,
		caCert:      caCert,
		caKey:       caKey,
		certs:       make(map[string]*tls.Certificate),
		serverCerts: make(map[string]*tls.Certificate),
		lnsConfigs:  make(map[string]*GatewayLNSConfig),
		dataURIs:    make(map[string]string),
	}
	return s, nil
}

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
func parsePrivateKey(block *pem.Block) (crypto.Signer, error) {
	switch block.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		switch k := key.(type) {
		case *ecdsa.PrivateKey:
			return k, nil
		case *rsa.PrivateKey:
			return k, nil
		default:
			return nil, fmt.Errorf("private key is not ECDSA or RSA (got %T)", key)
		}
	default:
		// Try formats in order
		if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
			return k, nil
		}
		if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
			return k, nil
		}
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		switch k := key.(type) {
		case *ecdsa.PrivateKey:
			return k, nil
		case *rsa.PrivateKey:
			return k, nil
		default:
			return nil, fmt.Errorf("private key is not ECDSA or RSA (got %T)", key)
		}
	}
}

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
	log.Infof("Created new CA in %s", dir)
	return caCert, caKey, nil
}

var maxSerial = new(big.Int).Lsh(big.NewInt(1), 128)

func writePEM(path, typ string, data []byte) error {
	return atomicCredentialWrite(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: data}))
}

func (s *CertStore) loadExistingGatewayCerts() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".crt") {
			continue
		}
		if strings.HasSuffix(e.Name(), ".server.crt") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".crt")
		if base == "ca" || base == "bootstrap" {
			continue
		}
		cert, err := s.loadGatewayCert(base)
		if err != nil {
			log.Warnf("Load gateway cert %s: %v", base, err)
			continue
		}
		s.certs[base] = cert
	}
	return nil
}

func (s *CertStore) loadExistingServerIssuedCerts() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".server.crt") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".server.crt")
		cert, err := s.loadServerIssuedCert(base)
		if err != nil {
			log.Warnf("Load server-issued cert %s: %v", base, err)
			continue
		}
		s.serverCerts[base] = cert
	}
	return nil
}

const dataURIFileSuffix = ".data_uri"
const lnsConfigFileSuffix = ".lns_config"

func (s *CertStore) loadExistingLNSConfigs() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), lnsConfigFileSuffix) {
			continue
		}
		gatewayID := strings.TrimSuffix(e.Name(), lnsConfigFileSuffix)
		path := filepath.Join(s.dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			log.Warnf("Load LNS config for gateway %s: %v", gatewayID, err)
			continue
		}
		var cfg GatewayLNSConfig
		if jsonErr := json.Unmarshal(b, &cfg); jsonErr != nil {
			log.Warnf("Parse LNS config for gateway %s: %v", gatewayID, jsonErr)
			continue
		}
		s.lnsConfigs[gatewayID] = &cfg
	}
	return nil
}

// GetGatewayLNSConfig returns the LNS-allocated config for the gateway (from router-info), if stored.
func (s *CertStore) GetGatewayLNSConfig(gatewayID string) (*GatewayLNSConfig, bool) {
	if s.Persistence != nil {
		r, e := s.Persistence.Load(s.storageContext(), gatewayID)
		return r.LNSConfig, e == nil && r.LNSConfig != nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.lnsConfigs[gatewayID]
	return cfg, ok && cfg != nil
}

// SetGatewayLNSConfig stores the LNS-allocated config for the gateway and persists it.
func (s *CertStore) SetGatewayLNSConfig(gatewayID string, cfg *GatewayLNSConfig) error {
	if s.Persistence != nil {
		return patchRecord(s.storageContext(), s.Persistence, gatewayID, GatewayRecordPatch{LNSConfig: cfg})
	}
	if gatewayID == "" || cfg == nil {
		return nil
	}
	path := filepath.Join(s.dir, gatewayID+lnsConfigFileSuffix)
	b, err := json.MarshalIndent(cfg, "", "\t")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0600); err != nil {
		return err
	}
	s.mu.Lock()
	s.lnsConfigs[gatewayID] = cfg
	// Keep legacy dataURIs in sync so GetGatewayDataURI still works
	if cfg.URI != "" {
		s.dataURIs[gatewayID] = cfg.URI
	}
	s.mu.Unlock()
	log.Debugf("Stored LNS config for gateway %s: uri=%s", gatewayID, cfg.URI)
	return nil
}

func (s *CertStore) loadExistingDataURIs() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), dataURIFileSuffix) {
			continue
		}
		gatewayID := strings.TrimSuffix(e.Name(), dataURIFileSuffix)
		path := filepath.Join(s.dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			log.Warnf("Load data URI for gateway %s: %v", gatewayID, err)
			continue
		}
		uri := strings.TrimSpace(string(b))
		if uri != "" {
			s.dataURIs[gatewayID] = uri
		}
	}
	return nil
}

// GatewayIDForClientCert returns the gateway ID that owns the given client certificate, by matching
// against stored certs (our-issued and server-issued). Use during TLS handshake to identify gateway from client cert.
func (s *CertStore) GatewayIDForClientCert(peerCert *x509.Certificate) string {
	if peerCert == nil || len(peerCert.Raw) == 0 {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, c := range s.certs {
		if len(c.Certificate) > 0 && bytes.Equal(peerCert.Raw, c.Certificate[0]) {
			return id
		}
	}
	for id, c := range s.serverCerts {
		if len(c.Certificate) > 0 && bytes.Equal(peerCert.Raw, c.Certificate[0]) {
			// serverCerts may be stored as gatewayID or gatewayID_tc
			return strings.TrimSuffix(id, "_tc")
		}
	}
	return ""
}

// GetGatewayDataURI returns the LNS data WebSocket URI for this gateway: from LNS config (router-info) if present, else legacy dataURIs.
func (s *CertStore) GetGatewayDataURI(gatewayID string) (string, bool) {
	if s.Persistence != nil {
		r, e := s.Persistence.Load(s.storageContext(), gatewayID)
		if e != nil {
			return "", false
		}
		if r.LNSConfig != nil && r.LNSConfig.URI != "" {
			return r.LNSConfig.URI, true
		}
		return strings.TrimSpace(r.DataURI), r.DataURI != ""
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if cfg, ok := s.lnsConfigs[gatewayID]; ok && cfg != nil && cfg.URI != "" {
		return cfg.URI, true
	}
	uri, ok := s.dataURIs[gatewayID]
	return uri, ok && uri != ""
}

// SetGatewayDataURI stores the LNS data URI for the gateway (from upstream router-info response) and persists it to certs-dir.
func (s *CertStore) SetGatewayDataURI(gatewayID string, uri string) error {
	if s.Persistence != nil {
		return patchRecord(s.storageContext(), s.Persistence, gatewayID, GatewayRecordPatch{DataURI: &uri})
	}
	if gatewayID == "" || uri == "" {
		return nil
	}
	path := filepath.Join(s.dir, gatewayID+dataURIFileSuffix)
	if err := os.WriteFile(path, []byte(uri+"\n"), 0600); err != nil {
		return err
	}
	s.mu.Lock()
	s.dataURIs[gatewayID] = uri
	s.mu.Unlock()
	log.Debugf("Stored data URI for gateway %s: %s", gatewayID, uri)
	return nil
}

func (s *CertStore) loadServerIssuedCert(gatewayID string) (*tls.Certificate, error) {
	certPath := filepath.Join(s.dir, gatewayID+".server.crt")
	keyPath := filepath.Join(s.dir, gatewayID+".server.key")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tlsCert, nil
}

func (s *CertStore) loadGatewayCert(gatewayID string) (*tls.Certificate, error) {
	if s.Persistence != nil {
		r, e := s.Persistence.Load(s.storageContext(), gatewayID)
		if errors.Is(e, ErrNotFound) {
			return nil, os.ErrNotExist
		}
		if e != nil {
			return nil, e
		}
		if len(r.Client.Cert) == 0 && len(r.Client.Key) > 0 {
			return nil, fmt.Errorf("incomplete gateway credential pair")
		}
		if len(r.Client.Cert) == 0 {
			return nil, os.ErrNotExist
		}
		c, e := tls.X509KeyPair(r.Client.Cert, r.Client.Key)
		return &c, e
	}

	certPath := filepath.Join(s.dir, gatewayID+".crt")
	keyPath := filepath.Join(s.dir, gatewayID+".key")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tlsCert, nil
}

// GetServerIssuedCert returns the server-issued (CUPS) client cert for the gateway if stored.
func (s *CertStore) GetServerIssuedCert(id string) (*tls.Certificate, bool) {
	if s.Persistence != nil {
		r, e := s.Persistence.Load(s.storageContext(), strings.TrimSuffix(id, "_tc"))
		if e != nil {
			return nil, false
		}
		blob := r.CUPS
		legacy := r.LegacyCUPS
		if strings.HasSuffix(id, "_tc") {
			blob = r.TC
			legacy = r.LegacyTC
		}
		if len(blob) > 0 {
			_, c, k, _, e := decodeCredential(blob)
			if e != nil || len(c) == 0 {
				return nil, false
			}
			legacy = CertificatePEM{pemCertificate(c), pemEncodeKey(k)}
		}
		c, e := tls.X509KeyPair(legacy.Cert, legacy.Key)
		return &c, e == nil
	}

	s.mu.RLock()
	c, ok := s.serverCerts[id]
	s.mu.RUnlock()
	if ok {
		return c, true
	}
	if blob, err := s.upstreamCredential(id); err == nil {
		_, cert, key, _, err := decodeCredential(blob)
		if err == nil && len(cert) == 0 {
			return nil, false
		}
		if err == nil && len(cert) > 0 {
			pair, e := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), pemEncodeKey(key))
			if e == nil {
				return &pair, true
			}
		}
	}
	c, err := s.loadServerIssuedCert(id)
	return c, err == nil
}

// PutServerIssuedCert stores the cert and key (DER from CUPS credentials blob) for the gateway.
// Used after intercepting the CUPS update-info response; these certs are then used for proxy→upstream auth.
func (s *CertStore) PutServerIssuedCert(gatewayID string, certDER, keyDER []byte) error {
	if s.Persistence != nil {
		pair := CertificatePEM{pemCertificate(certDER), pemEncodeKey(keyDER)}
		if _, e := tls.X509KeyPair(pair.Cert, pair.Key); e != nil {
			return e
		}
		p := GatewayRecordPatch{LegacyCUPS: &pair}
		if strings.HasSuffix(gatewayID, "_tc") {
			p = GatewayRecordPatch{LegacyTC: &pair}
		}
		return patchRecord(s.storageContext(), s.Persistence, strings.TrimSuffix(gatewayID, "_tc"), p)
	}

	if !s.writable {
		return fmt.Errorf("credential store is read-only")
	}
	tlsCert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		pemEncodeKey(keyDER),
	)
	if err != nil {
		return err
	}
	certPath := filepath.Join(s.dir, gatewayID+".server.crt")
	keyPath := filepath.Join(s.dir, gatewayID+".server.key")
	if err := writePEM(certPath, "CERTIFICATE", certDER); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, pemEncodeKey(keyDER), 0600); err != nil {
		return err
	}
	s.mu.Lock()
	s.serverCerts[gatewayID] = &tlsCert
	s.mu.Unlock()
	log.Infof("Stored server-issued client cert for gateway %s", gatewayID)
	return nil
}

// pemEncodeKey converts a private key DER (PKCS#8 or SEC1) to PEM bytes for storage and tls.X509KeyPair.
func pemEncodeKey(keyDER []byte) []byte {
	if key, err := x509.ParsePKCS8PrivateKey(keyDER); err == nil {
		switch k := key.(type) {
		case *ecdsa.PrivateKey:
			b, _ := x509.MarshalECPrivateKey(k)
			return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
		case *rsa.PrivateKey:
			b := x509.MarshalPKCS1PrivateKey(k)
			return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: b})
		}
	}
	if key, err := x509.ParseECPrivateKey(keyDER); err == nil {
		b, _ := x509.MarshalECPrivateKey(key)
		return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
	}
	if key, err := x509.ParsePKCS1PrivateKey(keyDER); err == nil {
		return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

// GetOrCreateClientCert returns a TLS certificate for use as client cert (proxy→upstream) for the given gateway.
// gatewayID must be a stable identity (e.g. normalized router from LNS); empty is rejected to avoid endpoint-based filenames.
func (s *CertStore) GetOrCreateClientCert(gatewayID string) (*tls.Certificate, error) {
	if gatewayID == "" {
		return nil, fmt.Errorf("gateway ID is required for client cert (use stable identity from router-info or client cert CN)")
	}
	s.mu.RLock()
	if c, ok := s.certs[gatewayID]; ok {
		s.mu.RUnlock()
		return c, nil
	}
	s.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.certs[gatewayID]; ok {
		return c, nil
	}
	tlsCert, err := s.loadGatewayCert(gatewayID)
	if err == nil {
		return tlsCert, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	tlsCert, err = s.issueAndSaveClientCert(gatewayID)
	if err != nil {
		return nil, err
	}
	// Read issued credentials on demand to keep gateway memory bounded.
	return tlsCert, nil
}

func (s *CertStore) issueAndSaveClientCert(gatewayID string) (*tls.Certificate, error) {
	if s.Persistence != nil && s.IssuerConfig != nil {
		b, e := IssueGatewayBootstrap(*s.IssuerConfig, gatewayID)
		if e != nil {
			return nil, e
		}
		pair := CertificatePEM{b.Cert, b.Key}
		for attempt := 0; attempt < 3; attempt++ {
			r, e := s.Persistence.Load(s.storageContext(), gatewayID)
			if errors.Is(e, ErrNotFound) {
				r = GatewayRecord{EUI: gatewayID}
			} else if e != nil {
				return nil, e
			}
			if len(r.Client.Cert) > 0 {
				c, e := tls.X509KeyPair(r.Client.Cert, r.Client.Key)
				return &c, e
			}
			r.Client = pair
			_, e = s.Persistence.Save(s.storageContext(), r, r.Revision)
			if errors.Is(e, ErrConflict) {
				continue
			}
			if e != nil {
				return nil, e
			}
			c, e := tls.X509KeyPair(pair.Cert, pair.Key)
			return &c, e
		}
		return nil, ErrConflict
	}
	if !s.writable || s.caCert == nil {
		return nil, fmt.Errorf("credential store does not permit issuing certificates")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, maxSerial)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: gatewayID},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(certValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, s.caCert, &key.PublicKey, s.caKey)
	if err != nil {
		return nil, err
	}
	certPath := filepath.Join(s.dir, gatewayID+".crt")
	keyPath := filepath.Join(s.dir, gatewayID+".key")
	if err := writePEM(certPath, "CERTIFICATE", der); err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER); err != nil {
		return nil, err
	}
	log.Infof("Issued client cert for gateway %s", gatewayID)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tlsCert, nil
}

// GetOrCreateBootstrap returns a server certificate for the proxy listen side (Station→proxy).
func (s *CertStore) GetOrCreateBootstrap() (*tls.Certificate, error) {
	certPath := filepath.Join(s.dir, bootstrapCert)
	keyPath := filepath.Join(s.dir, bootstrapKey)
	if data, err := os.ReadFile(certPath); err == nil {
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, err
		}
		tlsCert, err := tls.X509KeyPair(data, keyData)
		if err != nil {
			return nil, err
		}
		return &tlsCert, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, maxSerial)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "kudzu-forwarder-bs-bootstrap"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(certValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, s.caCert, &key.PublicKey, s.caKey)
	if err != nil {
		return nil, err
	}
	if err := writePEM(certPath, "CERTIFICATE", der); err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER); err != nil {
		return nil, err
	}
	log.Infof("Created bootstrap server cert in %s", s.dir)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tlsCert, nil
}

// CACertPath returns the path to the CA cert file (for documentation / stations to trust).
func (s *CertStore) CACertPath() string {
	return filepath.Join(s.dir, caCertFile)
}

// GetCACertDER returns the CA certificate as DER (for CUPS cred blob trust field).
// Stations that receive this as trust can verify the proxy's server cert when it is signed by this CA.
func (s *CertStore) GetCACertDER() []byte {
	if s.caCert == nil {
		return nil
	}
	return s.caCert.Raw
}

// ClientCACertPool returns an x509.CertPool containing the store's CA certificate, for use as
// tls.Config.ClientCAs so the server can request and verify client certificates (e.g. TC data connections).
func (s *CertStore) ClientCACertPool() *x509.CertPool {
	if s.caCert == nil {
		return nil
	}
	pool := x509.NewCertPool()
	pool.AddCert(s.caCert)
	return pool
}

// BootstrapCertPath returns the path to the bootstrap server cert file.
func (s *CertStore) BootstrapCertPath() string {
	return filepath.Join(s.dir, bootstrapCert)
}

// BootstrapKeyPath returns the path to the bootstrap server key file.
func (s *CertStore) BootstrapKeyPath() string {
	return filepath.Join(s.dir, bootstrapKey)
}

func (s *CertStore) storageContext() context.Context {
	if s.operationContext != nil {
		return s.operationContext
	}
	return context.Background()
}
