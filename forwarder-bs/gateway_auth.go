package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type gatewayAccess struct {
	Router           string   `json:"router"`
	Pins             []string `json:"client_cert_sha256"`
	IngressTokenFile string   `json:"ingress_token_file"`
	TCTokenFile      string   `json:"upstream_tc_token_file"`
	CUPSTokenFile    string   `json:"upstream_cups_token_file"`
	digest           [32]byte
	hasToken         bool
	tcAuth, cupsAuth string
}
type gatewayRegistry struct {
	gateways map[string]*gatewayAccess
	pins     map[string]string
}

func readAuthFile(path, base string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return "", e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("token file requires private permissions")
	}
	b, e := io.ReadAll(io.LimitReader(f, 8193))
	if e != nil {
		return "", e
	}
	if len(b) > 8192 {
		return "", fmt.Errorf("token too large")
	}
	token := strings.TrimSpace(string(b))
	if token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return "", fmt.Errorf("invalid token")
	}
	if !strings.Contains(token, " ") {
		token = "Bearer " + token
	}
	return token, nil
}
func loadGatewayRegistry(path string) (*gatewayRegistry, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return nil, fmt.Errorf("authorization manifest must be a regular file of at most 1 MiB")
	}
	var input struct {
		Version  int              `json:"version"`
		Gateways []*gatewayAccess `json:"gateways"`
	}
	dec := json.NewDecoder(io.LimitReader(f, 1<<20))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&input); e != nil {
		return nil, e
	}
	var extra interface{}
	if dec.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("trailing authorization data")
	}
	if input.Version != 1 || len(input.Gateways) == 0 || len(input.Gateways) > 4096 {
		return nil, fmt.Errorf("invalid gateway authorization manifest")
	}
	r := &gatewayRegistry{gateways: make(map[string]*gatewayAccess), pins: make(map[string]string)}
	for _, entry := range input.Gateways {
		id, e := ParseGatewayID(entry.Router)
		if e != nil {
			return nil, e
		}
		entry.Router = id
		if r.gateways[id] != nil {
			return nil, fmt.Errorf("duplicate gateway")
		}
		for _, pin := range entry.Pins {
			pin = strings.ToLower(pin)
			b, e := hex.DecodeString(pin)
			if e != nil || len(b) != 32 {
				return nil, fmt.Errorf("invalid certificate SHA256 pin")
			}
			if _, exists := r.pins[pin]; exists {
				return nil, fmt.Errorf("duplicate certificate pin")
			}
			r.pins[pin] = id
		}
		token, e := readAuthFile(entry.IngressTokenFile, filepath.Dir(path))
		if e != nil {
			return nil, e
		}
		if token != "" {
			entry.digest = sha256.Sum256([]byte(token))
			entry.hasToken = true
			for _, other := range r.gateways {
				if other.hasToken && other.digest == entry.digest {
					return nil, fmt.Errorf("duplicate ingress token")
				}
			}
		}
		if len(entry.Pins) == 0 && !entry.hasToken {
			return nil, fmt.Errorf("gateway requires ingress proof")
		}
		entry.tcAuth, e = readAuthFile(entry.TCTokenFile, filepath.Dir(path))
		if e != nil {
			return nil, e
		}
		entry.cupsAuth, e = readAuthFile(entry.CUPSTokenFile, filepath.Dir(path))
		if e != nil {
			return nil, e
		}
		r.gateways[id] = entry
	}
	return r, nil
}
func (r *gatewayRegistry) authorize(req *http.Request, claimed string, store *CertStore) (string, error) {
	if req.TLS == nil {
		return "", fmt.Errorf("shared access requires TLS")
	}
	certID, tokenID := "", ""
	if len(req.TLS.PeerCertificates) > 0 {
		cert := req.TLS.PeerCertificates[0]
		now := time.Now()
		if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			return "", fmt.Errorf("expired ingress certificate")
		}
		usage := len(cert.ExtKeyUsage) == 0
		for _, u := range cert.ExtKeyUsage {
			usage = usage || u == x509.ExtKeyUsageClientAuth || u == x509.ExtKeyUsageAny
		}
		if !usage {
			return "", fmt.Errorf("certificate is not valid for client authentication")
		}
		digest := sha256.Sum256(cert.Raw)
		certID = r.pins[hex.EncodeToString(digest[:])]
		if certID == "" && store != nil {
			candidate := NormalizeGatewayID(claimed)
			if candidate == "" {
				candidate = NormalizeGatewayID(cert.Subject.CommonName)
			}
			if r.gateways[candidate] != nil {
				stored, err := store.loadGatewayCert(candidate)
				if err == nil && len(stored.Certificate) > 0 && bytes.Equal(stored.Certificate[0], cert.Raw) {
					certID = candidate
				}
			}
		}
		if certID == "" {
			return "", fmt.Errorf("unregistered ingress certificate")
		}
	}
	auth := req.Header.Get("Authorization")
	if auth != "" {
		if len(auth) > 8192 {
			return "", fmt.Errorf("authorization too large")
		}
		digest := sha256.Sum256([]byte(auth))
		for id, entry := range r.gateways {
			if entry.hasToken && subtle.ConstantTimeCompare(digest[:], entry.digest[:]) == 1 {
				tokenID = id
			}
		}
		if tokenID == "" {
			return "", fmt.Errorf("unregistered ingress token")
		}
	}
	id := certID
	if id == "" {
		id = tokenID
	}
	if id == "" || (certID != "" && tokenID != "" && certID != tokenID) {
		return "", fmt.Errorf("gateway authentication required")
	}
	if claimed != "" {
		canonical, e := ParseGatewayID(claimed)
		if e != nil || canonical != id {
			return "", fmt.Errorf("router differs from authenticated gateway")
		}
	}
	return id, nil
}
func (p *WSProxy) authorize(req *http.Request, claimed string) (string, error) {
	if p.auth != nil {
		return p.auth.authorize(req, claimed, p.config.CertStore)
	}
	if claimed == "" {
		claimed = p.config.LocalRouter
	}
	return ParseGatewayID(claimed)
}
func (p *WSProxy) upstreamHeaders(id, purpose string, in http.Header) http.Header {
	h := make(http.Header)
	if p.auth == nil {
		if v := in.Get("Authorization"); v != "" {
			h.Set("Authorization", v)
		}
	} else if entry := p.auth.gateways[id]; entry != nil {
		v := entry.tcAuth
		if purpose == "cups" {
			v = entry.cupsAuth
		}
		if v != "" {
			h.Set("Authorization", v)
		}
	}
	if p.config.CertStore != nil {
		suffix := id
		if purpose == "tc" {
			suffix += "_tc"
		}
		if _, err := p.config.CertStore.upstreamCredential(suffix); err == nil {
			h.Del("Authorization")
			if token := p.config.CertStore.upstreamToken(suffix); token != "" {
				h.Set("Authorization", token)
			}
		}
	}
	return h
}
func upstreamOrigin(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil {
		return ""
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return ""
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "wss" {
			port = "443"
		}
	}
	return u.Scheme + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}
func (c *WSProxyConfig) allowedUpstream(raw string) bool {
	origin := upstreamOrigin(raw)
	if origin == "" {
		return false
	}
	values := []string{c.ConnectURI, c.ConnectDataURI, c.ConnectDiscoveryURI}
	values = append(values, strings.Split(c.UpstreamAllowedOrigins, ",")...)
	for _, v := range values {
		if upstreamOrigin(strings.TrimSpace(v)) == origin {
			return true
		}
	}
	return false
}
