package forwarderbs

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"
)

const certValidity = 365 * 24 * time.Hour

var maxSerial = new(big.Int).Lsh(big.NewInt(1), 128)

// CertStore manages gateway credentials through record persistence.
type CertStore struct {
	operationContext context.Context
	Persistence      GatewayPersistence
	IssuerConfig     *ForwarderConfig
	caCert           *x509.Certificate
	mu               sync.Mutex
}

// NewCertStore constructs record-backed credential access with optional issuance.
func NewCertStore(persistence GatewayPersistence, c ForwarderConfig) (*CertStore, error) {
	if persistence == nil {
		return nil, fmt.Errorf("gateway persistence is required")
	}
	trust, e := material(c.CertsCACertPEM, c.CertsCACert)
	if e != nil {
		return nil, e
	}
	var ca *x509.Certificate
	if len(trust) > 0 {
		block, _ := pem.Decode(trust)
		if block == nil {
			return nil, fmt.Errorf("invalid CA PEM")
		}
		ca, e = x509.ParseCertificate(block.Bytes)
		if e != nil {
			return nil, e
		}
	}
	return &CertStore{Persistence: persistence, IssuerConfig: &c, caCert: ca}, nil
}
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
func (s *CertStore) storageContext() context.Context {
	if s.operationContext != nil {
		return s.operationContext
	}
	return context.Background()
}
func (s *CertStore) GetCACertDER() []byte {
	if s.caCert == nil {
		return nil
	}
	return s.caCert.Raw
}

// ClientCACertPool returns an x509.CertPool containing the store's CA certificate, for use as
// tls.Config.ClientCAs so the server can request and verify client certificates (e.g. TC data connections).
func (s *CertStore) loadGatewayCert(id string) (*tls.Certificate, error) {
	r, e := s.Persistence.Load(s.storageContext(), id)
	if e != nil {
		return nil, e
	}
	if len(r.Client.Cert) == 0 && len(r.Client.Key) == 0 {
		return nil, ErrNotFound
	}
	c, e := tls.X509KeyPair(r.Client.Cert, r.Client.Key)
	return &c, e
}
func (s *CertStore) GetServerIssuedCert(id string) (*tls.Certificate, bool) {
	blob, e := s.upstreamCredential(id)
	if e != nil {
		return nil, false
	}
	_, cert, key, _, e := decodeCredential(blob)
	if e != nil || len(cert) == 0 {
		return nil, false
	}
	c, e := tls.X509KeyPair(pemCertificate(cert), pemEncodeKey(key))
	return &c, e == nil
}
func (s *CertStore) GetOrCreateClientCert(id string) (*tls.Certificate, error) {
	if _, e := ParseGatewayID(id); e != nil {
		return nil, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, e := s.loadGatewayCert(id)
	if e == nil {
		return c, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return nil, e
	}
	if s.IssuerConfig == nil {
		return nil, ErrIssuanceDisabled
	}
	b, e := IssueGatewayBootstrap(*s.IssuerConfig, id)
	if e != nil {
		return nil, e
	}
	pair := CertificatePEM{b.Cert, b.Key}
	for attempt := 0; attempt < 3; attempt++ {
		r, e := s.Persistence.Load(s.storageContext(), id)
		if errors.Is(e, ErrNotFound) {
			r = GatewayRecord{EUI: id}
		} else if e != nil {
			return nil, e
		}
		if len(r.Client.Cert) > 0 || len(r.Client.Key) > 0 {
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
func (s *CertStore) GetGatewayLNSConfig(id string) (*GatewayLNSConfig, bool) {
	r, e := s.Persistence.Load(s.storageContext(), id)
	return r.LNSConfig, e == nil && r.LNSConfig != nil
}
func (s *CertStore) SetGatewayLNSConfig(id string, c *GatewayLNSConfig) error {
	return patchRecord(s.storageContext(), s.Persistence, id, GatewayRecordPatch{LNSConfig: c})
}
func (s *CertStore) GetGatewayDataURI(id string) (string, bool) {
	c, ok := s.GetGatewayLNSConfig(id)
	if !ok {
		return "", false
	}
	return c.URI, c.URI != ""
}
