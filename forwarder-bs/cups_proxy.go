package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type cupsRequest struct {
	Router  json.RawMessage `json:"router"`
	CupsURI string          `json:"cupsUri"`
	TcURI   *string         `json:"tcUri"`
	CupsCRC *uint32         `json:"cupsCredCrc"`
	TcCRC   *uint32         `json:"tcCredCrc"`
	Station string          `json:"station,omitempty"`
	Model   string          `json:"model,omitempty"`
	Package *string         `json:"package"`
	Keys    []uint32        `json:"keys"`
}

func (p *WSProxy) gatewayDialer(id, purpose string, data bool) (*tls.Config, error) {
	cert := p.config.clientCertForGatewayWebSocket(id)
	if purpose == "cups" {
		cert = nil
		if p.config.CertStore != nil {
			cert, _ = p.config.CertStore.GetServerIssuedCert(id)
			if cert == nil && p.config.CertsUpstreamClientCert {
				cert, _ = p.config.CertStore.GetOrCreateClientCert(id)
			}
		}
	}
	dialer, err := p.config.dialerFor(cert, data)
	if err != nil {
		return nil, err
	}
	conf := dialer.TLSClientConfig
	if store := p.config.CertStore; store != nil {
		suffix := id
		if purpose == "tc" {
			suffix += "_tc"
		}
		if blob, err := store.upstreamCredential(suffix); err == nil {
			trust, _, _, _, err := decodeCredential(blob)
			if err != nil {
				return nil, err
			}
			c, err := x509.ParseCertificate(trust)
			if err != nil {
				return nil, err
			}
			conf.RootCAs.AddCert(c)
		}
	}
	return conf, nil
}
func readCUPSPrefix(r io.Reader, limit int) (*ParsedCUPSResponse, int, error) {
	out := &ParsedCUPSResponse{}
	consumed := 0
	read := func(width int) ([]byte, error) {
		b := make([]byte, width)
		_, e := io.ReadFull(r, b)
		consumed += len(b)
		return b, e
	}
	for i := 0; i < 4; i++ {
		width := 1
		if i > 1 {
			width = 2
		}
		header, e := read(width)
		if e != nil {
			return nil, consumed, e
		}
		n := int(header[0])
		if width == 2 {
			n = int(binary.LittleEndian.Uint16(header))
			if n > limit {
				return nil, consumed, fmt.Errorf("credential exceeds configured limit")
			}
		}
		data, e := read(n)
		if e != nil {
			return nil, consumed, e
		}
		switch i {
		case 0:
			out.CupsURI = string(data)
		case 1:
			out.TcURI = string(data)
		case 2:
			out.CupsCred = data
		case 3:
			out.TcCred = data
		}
	}
	return out, consumed, nil
}
func readCUPSTrailer(r io.Reader) (header []byte, updateLength uint32, err error) {
	var word [4]byte
	if _, err = io.ReadFull(r, word[:]); err != nil {
		return
	}
	n := binary.LittleEndian.Uint32(word[:])
	if n > 4096 || (n > 0 && n < 8) {
		err = fmt.Errorf("invalid signature length")
		return
	}
	header = append(header, word[:]...)
	signature := make([]byte, int(n))
	if _, err = io.ReadFull(r, signature); err != nil {
		return
	}
	header = append(header, signature...)
	if _, err = io.ReadFull(r, word[:]); err != nil {
		return
	}
	updateLength = binary.LittleEndian.Uint32(word[:])
	header = append(header, word[:]...)
	return
}
func (p *WSProxy) validateManagedCUPS() error {
	if p.config.CUPSMode != "managed" {
		return nil
	}
	store := p.config.CertStore
	if store == nil || store.caCert == nil || !store.writable {
		return fmt.Errorf("managed CUPS requires a writable CA credential store")
	}
	pair, err := tls.LoadX509KeyPair(p.config.ListenTLSCert, p.config.ListenTLSKey)
	if err != nil {
		return err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	public, err := url.Parse(p.publicURI())
	if err != nil || public.Scheme != "wss" {
		return fmt.Errorf("managed CUPS requires a wss public-uri")
	}
	roots := x509.NewCertPool()
	roots.AddCert(store.caCert)
	intermediates := x509.NewCertPool()
	for _, der := range pair.Certificate[1:] {
		c, e := x509.ParseCertificate(der)
		if e != nil {
			return e
		}
		intermediates.AddCert(c)
	}
	_, err = leaf.Verify(x509.VerifyOptions{DNSName: public.Hostname(), Roots: roots, Intermediates: intermediates})
	return err
}
func (p *WSProxy) handleHTTPProxy(w http.ResponseWriter, r *http.Request) {
	if p.config.CUPSMode != "managed" || r.Method != http.MethodPost || r.URL.Path != "/update-info" {
		http.NotFound(w, r)
		return
	}
	if !p.admit(w) {
		return
	}
	defer p.release()
	var incoming cupsRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	if err := dec.Decode(&incoming); err != nil {
		http.Error(w, "invalid CUPS request", 400)
		return
	}
	var extra interface{}
	if dec.Decode(&extra) != io.EOF {
		http.Error(w, "trailing request data", 400)
		return
	}
	claimed, err := parseRouterJSON(incoming.Router)
	if err != nil {
		http.Error(w, "invalid router", 400)
		return
	}
	id, err := p.authorize(r, claimed)
	if err != nil {
		http.Error(w, "gateway authorization rejected", 403)
		return
	}
	store := p.config.CertStore
	state := store.cupsState(id)
	base, err := url.Parse(p.config.ConnectURI)
	if err != nil {
		http.Error(w, "invalid upstream", 502)
		return
	}
	scheme := "https"
	if base.Scheme == "ws" {
		scheme = "http"
	}
	upstreamBase := scheme + "://" + base.Host
	if state.CupsURI != "" {
		upstreamBase = state.CupsURI
	}
	upstreamURL := strings.TrimRight(upstreamBase, "/") + "/update-info"
	if !p.config.allowedUpstream(upstreamURL) {
		http.Error(w, "unapproved upstream origin", 502)
		return
	}
	outgoing := incoming
	outgoing.Router, _ = json.Marshal(gatewayIDToEUIFormat(id))
	outgoing.CupsURI = upstreamBase
	tcURI := state.TcURI
	if tcURI == "" {
		tcURI = p.config.ConnectURI
	}
	outgoing.TcURI = &tcURI
	cupsCRC, tcCRC := store.upstreamCRC(id), store.upstreamCRC(id+"_tc")
	outgoing.CupsCRC = &cupsCRC
	outgoing.TcCRC = &tcCRC
	body, err := json.Marshal(outgoing)
	if err != nil {
		http.Error(w, "invalid CUPS body", 400)
		return
	}
	conf, err := p.gatewayDialer(id, "cups", false)
	if err != nil {
		http.Error(w, "upstream TLS configuration failed", 502)
		return
	}
	transport := &http.Transport{TLSClientConfig: conf, MaxIdleConns: 1, MaxIdleConnsPerHost: 1}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: httpClientTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(p.root, httpClientTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "invalid upstream request", 502)
		return
	}
	request.Header = p.upstreamHeaders(id, "cups", r.Header)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		http.Error(w, "upstream CUPS request failed", 502)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		http.Error(w, "upstream CUPS request rejected", response.StatusCode)
		return
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/octet-stream" || response.ContentLength < 14 {
		http.Error(w, "CUPS requires a known-length binary response", 502)
		return
	}
	parsed, originalPrefix, err := readCUPSPrefix(response.Body, p.config.CUPSMaxCredentialBytes)
	if err != nil {
		http.Error(w, "invalid CUPS credential prefix", 502)
		return
	}
	trailer, updateLength, err := readCUPSTrailer(response.Body)
	if err != nil || int64(originalPrefix+len(trailer))+int64(updateLength) != response.ContentLength {
		http.Error(w, "invalid CUPS response lengths", 502)
		return
	}
	for _, uri := range []string{parsed.CupsURI, parsed.TcURI} {
		if uri != "" && !p.config.allowedUpstream(uri) {
			http.Error(w, "unapproved CUPS endpoint", 502)
			return
		}
	}
	if parsed.CupsURI != "" {
		state.CupsURI = parsed.CupsURI
	}
	if parsed.TcURI != "" {
		state.TcURI = parsed.TcURI
		p.setRoute(id, "")
	}
	for _, blob := range [][]byte{parsed.CupsCred, parsed.TcCred} {
		if len(blob) > 0 {
			if _, _, _, _, e := decodeCredential(blob); e != nil {
				http.Error(w, "invalid upstream credentials", 502)
				return
			}
		}
	}
	for _, entry := range []struct {
		suffix string
		blob   []byte
	}{{id, parsed.CupsCred}, {id + "_tc", parsed.TcCred}} {
		if len(entry.blob) > 0 {
			if err = store.putUpstreamCredential(entry.suffix, entry.blob); err != nil {
				http.Error(w, "invalid upstream credentials", 502)
				return
			}
		}
	}
	if parsed.CupsURI != "" || parsed.TcURI != "" {
		if err = store.putCUPSState(id, state); err != nil {
			http.Error(w, "cannot persist CUPS state", 500)
			return
		}
	}
	issued, err := store.GetOrCreateClientCert(id)
	if err != nil {
		http.Error(w, "cannot load station credentials", 500)
		return
	}
	key, err := KeyDERForCupsBlob(issued.PrivateKey)
	if err != nil {
		http.Error(w, "cannot encode station credentials", 500)
		return
	}
	credentials := CredBlobFromTrustCertKey(store.GetCACertDER(), issued.Certificate[0], key)
	if len(credentials) > p.config.CUPSMaxCredentialBytes {
		http.Error(w, "station credentials exceed configured limit", 500)
		return
	}
	crc := crc32.ChecksumIEEE(credentials)
	publicTC := p.publicURI()
	publicCUPS := strings.Replace(strings.Replace(publicTC, "wss://", "https://", 1), "ws://", "http://", 1)
	cupsURI, tcUpdate := "", ""
	if incoming.CupsURI != publicCUPS {
		cupsURI = publicCUPS
	}
	if incoming.TcURI == nil || *incoming.TcURI != publicTC {
		tcUpdate = publicTC
	}
	var cupsCred, tcCred []byte
	if incoming.CupsCRC == nil || *incoming.CupsCRC != crc {
		cupsCred = credentials
	}
	if incoming.TcCRC == nil || *incoming.TcCRC != crc {
		tcCred = credentials
	}
	prefix, err := BuildCupsResponseFull(cupsURI, tcUpdate, cupsCred, tcCred, trailer)
	if err != nil {
		http.Error(w, "cannot encode CUPS update", 500)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(int64(len(prefix))+int64(updateLength), 10))
	w.WriteHeader(200)
	if _, err = w.Write(prefix); err != nil {
		return
	}
	var buffer [4096]byte
	_, _ = io.CopyBuffer(w, io.LimitReader(response.Body, int64(updateLength)), buffer[:])
}
