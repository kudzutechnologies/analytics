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

	log "github.com/sirupsen/logrus"
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
	id := "?"
	reject := func(status int, reason string) {
		log.Warnf("%sCUPS failed status=%d reason=%s", gatewayLogPrefix(id), status, reason)
		http.Error(w, reason, status)
	}
	var incoming cupsRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	if err := dec.Decode(&incoming); err != nil {
		reject(400, "invalid CUPS request")
		return
	}
	var extra interface{}
	if dec.Decode(&extra) != io.EOF {
		reject(400, "trailing request data")
		return
	}
	claimed, err := parseRouterJSON(incoming.Router)
	if err != nil {
		reject(400, "invalid router")
		return
	}
	id, err = p.authorize(r, claimed)
	if err != nil {
		id = "?"
		reject(403, "gateway authorization rejected")
		return
	}
	store := p.config.CertStore
	state := store.cupsState(id)
	base, err := url.Parse(p.config.ConnectURI)
	if err != nil {
		reject(502, "invalid upstream")
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
		reject(502, "unapproved upstream origin")
		return
	}
	outgoing := incoming
	outgoing.Router, _ = json.Marshal(gatewayIDToEUIFormat(id))
	outgoing.CupsURI = upstreamBase
	tcURI := state.TcURI
	if tcURI == "" {
		// connect-uri names CUPS in managed mode, not an assigned LNS.
		// Report an empty TC URI during bootstrap unless separately configured.
		tcURI = p.config.ConnectDataURI
	}
	outgoing.TcURI = &tcURI
	cupsCRC, tcCRC := store.upstreamCRC(id), store.upstreamCRC(id+"_tc")
	outgoing.CupsCRC = &cupsCRC
	outgoing.TcCRC = &tcCRC
	log.Infof("%sCUPS upstream request origin=%s cups_crc=%08x tc_crc=%08x",
		gatewayLogPrefix(id), upstreamLogOrigin(upstreamURL), cupsCRC, tcCRC)
	body, err := json.Marshal(outgoing)
	if err != nil {
		reject(400, "invalid CUPS body")
		return
	}
	conf, err := p.gatewayDialer(id, "cups", false)
	if err != nil {
		reject(502, "upstream TLS configuration failed")
		return
	}
	transport := &http.Transport{TLSClientConfig: conf, MaxIdleConns: 1, MaxIdleConnsPerHost: 1}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: httpClientTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(p.root, httpClientTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		reject(502, "invalid upstream request")
		return
	}
	request.Header = p.upstreamHeaders(id, "cups", r.Header)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		log.Warnf("%sCUPS upstream connection failed reason=%s", gatewayLogPrefix(id), upstreamFailureReason(err))
		reject(502, "upstream CUPS request failed")
		return
	}
	defer response.Body.Close()
	log.Infof("%sCUPS upstream response status=%d bytes=%d", gatewayLogPrefix(id), response.StatusCode, response.ContentLength)
	if response.StatusCode != 200 {
		reject(response.StatusCode, "upstream CUPS request rejected")
		return
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/octet-stream" || response.ContentLength < 14 {
		reject(502, "CUPS requires a known-length binary response")
		return
	}
	parsed, originalPrefix, err := readCUPSPrefix(response.Body, p.config.CUPSMaxCredentialBytes)
	if err != nil {
		reject(502, "invalid CUPS credential prefix")
		return
	}
	trailer, updateLength, err := readCUPSTrailer(response.Body)
	if err != nil || int64(originalPrefix+len(trailer))+int64(updateLength) != response.ContentLength {
		reject(502, "invalid CUPS response lengths")
		return
	}
	log.Infof("%sCUPS upstream update cups_origin=%s tc_origin=%s cups_credential_bytes=%d tc_credential_bytes=%d firmware_bytes=%d",
		gatewayLogPrefix(id), upstreamLogOrigin(parsed.CupsURI), upstreamLogOrigin(parsed.TcURI), len(parsed.CupsCred), len(parsed.TcCred), updateLength)
	if cupsCRC == 0 && tcCRC == 0 && parsed.CupsURI == "" && parsed.TcURI == "" && len(parsed.CupsCred) == 0 && len(parsed.TcCred) == 0 && updateLength == 0 {
		log.Warnf("%sCUPS upstream returned no updates with zero local upstream credential CRCs; check ThingPark registration and bootstrap state", gatewayLogPrefix(id))
	}
	for _, uri := range []string{parsed.CupsURI, parsed.TcURI} {
		if uri != "" && !p.config.allowedUpstream(uri) {
			reject(502, "unapproved CUPS endpoint")
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
				reject(502, "invalid upstream credentials")
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
				reject(502, "invalid upstream credentials")
				return
			}
		}
	}
	if parsed.CupsURI != "" || parsed.TcURI != "" {
		if err = store.putCUPSState(id, state); err != nil {
			reject(500, "cannot persist CUPS state")
			return
		}
	}
	issued, err := store.GetOrCreateClientCert(id)
	if err != nil {
		reject(500, "cannot load station credentials")
		return
	}
	key, err := KeyDERForCupsBlob(issued.PrivateKey)
	if err != nil {
		reject(500, "cannot encode station credentials")
		return
	}
	credentials := CredBlobFromTrustCertKey(store.GetCACertDER(), issued.Certificate[0], key)
	if len(credentials) > p.config.CUPSMaxCredentialBytes {
		reject(500, "station credentials exceed configured limit")
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
		reject(500, "cannot encode CUPS update")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(int64(len(prefix))+int64(updateLength), 10))
	w.WriteHeader(200)
	if _, err = w.Write(prefix); err != nil {
		log.Warnf("%sCUPS response write failed", gatewayLogPrefix(id))
		return
	}
	var buffer [4096]byte
	written, copyErr := io.CopyBuffer(w, io.LimitReader(response.Body, int64(updateLength)), buffer[:])
	if copyErr != nil || written != int64(updateLength) {
		log.Warnf("%sCUPS response transfer incomplete firmware_bytes=%d expected=%d", gatewayLogPrefix(id), written, updateLength)
		return
	}
	log.Infof("%sCUPS response sent cups_uri_updated=%t tc_uri_updated=%t cups_credential_bytes=%d tc_credential_bytes=%d",
		gatewayLogPrefix(id), cupsURI != "", tcUpdate != "", len(cupsCred), len(tcCred))
}
