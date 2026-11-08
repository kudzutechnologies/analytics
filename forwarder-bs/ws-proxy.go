package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	log "github.com/sirupsen/logrus"
)

const (
	writeWait          = 10 * time.Second
	pongWait           = 60 * time.Second
	pingPeriod         = (pongWait * 9) / 10
	maxMessageSize     = 65536
	maxDialRedirect    = 5
	httpClientTimeout  = 30 * time.Second
	wsHandshakeTimeout = 10 * time.Second
	httpServerTimeout  = 15 * time.Second
)

// WSProxyEvents is the interface for analytics extraction on proxied messages.
type WSProxyEvents interface {
	OnUpstreamMessage(gatewayKey string, raw []byte, isBinary bool)
	OnDownstreamMessage(gatewayKey string, raw []byte, isBinary bool)
	SetDRTable(gatewayKey string, drTable DRTable)
}

// WSProxyConfig holds configuration for the WebSocket proxy.
type WSProxyConfig struct {
	PingInterval, PongTimeout time.Duration

	ProxyMode, PublicURI, GatewayAuthFile, LocalRouter, UpstreamAllowedOrigins, CUPSMode string
	InjectRouterIntoVersion                                                              bool

	ListenHost               string
	ListenPort               int
	ConnectURI               string
	ConnectDiscoveryURI      string // optional; full router-info WebSocket URI
	ConnectDiscoveryPath     string // optional; router-info path on connect-uri host (default /router-info)
	ConnectDataPath          string // optional; path for LNS data WebSocket (e.g. /tpe/lns when root returns HTML)
	ConnectDataURI           string // optional; full LNS data WebSocket URI (overrides connect-uri + connect-data-path when set)
	ConnectWSSubprotocol     string // optional, e.g. "lora.v2" for some LNS
	ListenTLSCert            string
	ListenTLSKey             string
	ConnectTLSCA             string
	ConnectTLSInsecure       bool
	ConnectDataTLSServerName string // optional; override TLS ServerName for data connection when server cert uses a different hostname (e.g. lns-tpw.thingpark.com)
	ConnectDataTLSCA         string // optional; CA file for data connection TLS (when LNS uses a different CA than connect-uri)
	ConnectDataTLSInsecure   bool   // if true, skip server certificate verification for data connection only (client cert still sent; use when LNS uses an internal/unknown CA)
	MaxStreams               int
	Events                   WSProxyEvents
	DebugDump                *DebugDumpWriter
	CertStore                *CertStore // optional: per-gateway client certs for CUPS upstream
	CertsUpstreamClientCert  bool       // if true, use proxy-issued client cert for CUPS when no server-issued; only when upstream trusts proxy CA
	Verbose                  bool       // if true, hexdump incoming/outgoing traffic (implies debug)
}

// WSProxy is the WebSocket man-in-the-middle proxy.
type WSProxy struct {
	routesMu  sync.Mutex
	routes    map[string]string
	pendingMu sync.Mutex
	pending   map[*websocket.Conn]struct{}
	admission chan struct{}
	root      context.Context
	cancel    context.CancelFunc

	auth *gatewayRegistry

	config     *WSProxyConfig
	server     *http.Server
	upgrader   websocket.Upgrader
	sessions   *sessionRegistry
	httpClient *http.Client
}

// SetEvents sets the event handler for analytics (can be called after creation).
func (p *WSProxy) SetEvents(events WSProxyEvents) {
	p.config.Events = events
}

// session holds a gateway connection and its upstream connection.
type session struct {
	gatewayKey   string
	gatewayConn  *websocket.Conn
	upstreamConn *websocket.Conn
	config       *WSProxyConfig
	events       WSProxyEvents
	closeOnce    sync.Once
	done         chan struct{}
}

// logCertDetails logs certificate details at debug level.
func logCertDetails(prefix string, certs []*x509.Certificate) {
	if len(certs) == 0 || !log.IsLevelEnabled(log.DebugLevel) {
		return
	}
	for i, c := range certs {
		subj := ""
		if len(c.Subject.CommonName) > 0 {
			subj = c.Subject.CommonName
		} else {
			subj = c.Subject.String()
		}
		issuer := ""
		if len(c.Issuer.CommonName) > 0 {
			issuer = c.Issuer.CommonName
		} else {
			issuer = c.Issuer.String()
		}
		log.Debugf("[cert %s #%d] version=%d serial=%s subject=%q issuer=%q notBefore=%s notAfter=%s dns=%v",
			prefix, i+1, c.Version+1, c.SerialNumber.String(), subj, issuer,
			c.NotBefore.Format(time.RFC3339), c.NotAfter.Format(time.RFC3339), c.DNSNames)
	}
}

// loadAndLogCertsFromPEM parses PEM data, logs each certificate at debug level, returns certs.
func loadAndLogCertsFromPEM(prefix string, pemData []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	for len(pemData) > 0 {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse cert: %w", err)
		}
		certs = append(certs, c)
	}
	logCertDetails(prefix, certs)
	return certs, nil
}

// dialer returns a WebSocket dialer for the upstream. If clientCert is non-nil it is used for TLS client auth
// (required by many LNS e.g. ThingPark for router-info and data WebSocket).
func (p *WSProxyConfig) dialer(cert *tls.Certificate) (*websocket.Dialer, error) {
	return p.dialerFor(cert, false)
}
func (p *WSProxyConfig) dialerFor(cert *tls.Certificate, data bool) (*websocket.Dialer, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	if roots == nil {
		roots = x509.NewCertPool()
	}
	ca := p.ConnectTLSCA
	insecure := p.ConnectTLSInsecure
	name := ""
	if data {
		if p.ConnectDataTLSCA != "" {
			ca = p.ConnectDataTLSCA
		}
		insecure = insecure || p.ConnectDataTLSInsecure
		name = p.ConnectDataTLSServerName
	}
	if ca != "" {
		raw, err := os.ReadFile(ca)
		if err != nil {
			return nil, err
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, fmt.Errorf("invalid upstream CA")
		}
	}
	conf := &tls.Config{RootCAs: roots, InsecureSkipVerify: insecure, ServerName: name, MinVersion: tls.VersionTLS12}
	if cert != nil {
		conf.Certificates = []tls.Certificate{*cert}
	}
	return &websocket.Dialer{HandshakeTimeout: wsHandshakeTimeout, TLSClientConfig: conf}, nil
}

// clientCertForGateway returns the TLS client cert to use for upstream (router-info / data) for the given gateway.
// Same precedence as in handleHTTPProxy: server-issued, then server-issued TC, then our-issued if CertsUpstreamClientCert.
func (p *WSProxyConfig) clientCertForGateway(gatewayID string) *tls.Certificate {
	if p.CertStore == nil || gatewayID == "" {
		return nil
	}
	if c, ok := p.CertStore.GetServerIssuedCert(gatewayID + "_tc"); ok {
		return c
	}
	if p.CertsUpstreamClientCert {
		c, _ := p.CertStore.GetOrCreateClientCert(gatewayID)
		return c
	}
	return nil
}
func (p *WSProxyConfig) clientCertForGatewayWebSocket(gatewayID string) *tls.Certificate {
	return p.clientCertForGateway(gatewayID)
}

// buildHTTPClient returns an HTTP client for proxying CUPS and other non-WebSocket requests (same TLS as dialer).
// If clientCert is non-nil, it is used as the TLS client certificate for the upstream connection (e.g. per-gateway CUPS).
func (p *WSProxyConfig) buildHTTPClient(clientCert *tls.Certificate) *http.Client {
	transport := &http.Transport{}
	connectURL, err := url.Parse(p.ConnectURI)
	if err != nil {
		return &http.Client{Transport: transport, Timeout: httpClientTimeout}
	}
	tlsConfig := &tls.Config{
		InsecureSkipVerify: p.ConnectTLSInsecure,
		ServerName:         connectURL.Hostname(),
	}
	if p.ConnectTLSCA != "" {
		b, err := os.ReadFile(p.ConnectTLSCA)
		if err != nil {
			return &http.Client{Transport: transport, Timeout: httpClientTimeout}
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(b)
		tlsConfig.RootCAs = pool
	}
	if clientCert != nil {
		tlsConfig.Certificates = []tls.Certificate{*clientCert}
	}
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport, Timeout: httpClientTimeout}
}

// dialWithRedirects dials the upstream WebSocket, following 301/302/307/308 redirects.
// On non-redirect failure it returns an error whose string includes " — HTTP %d — body: ..." so callers can detect 200 (HTML) and try another path.
type upstreamHandshakeError struct {
	status int
	err    error
}

func (e *upstreamHandshakeError) Error() string {
	return fmt.Sprintf("upstream handshake HTTP %d: %v", e.status, e.err)
}
func (p *WSProxyConfig) dialWithRedirects(d *websocket.Dialer, uri string, h http.Header) (*websocket.Conn, error) {
	return p.dialContextWithRedirects(context.Background(), d, uri, h)
}
func (p *WSProxyConfig) dialContextWithRedirects(ctx context.Context, d *websocket.Dialer, uri string, h http.Header) (*websocket.Conn, error) {
	for i := 0; i < maxDialRedirect; i++ {
		if !p.allowedUpstream(uri) {
			return nil, fmt.Errorf("upstream origin is not allowed")
		}
		c, resp, err := d.DialContext(ctx, uri, h)
		if err == nil {
			return c, nil
		}
		if resp == nil {
			return nil, err
		}
		resp.Body.Close()
		code := resp.StatusCode
		if code != 301 && code != 302 && code != 307 && code != 308 {
			return nil, &upstreamHandshakeError{status: code, err: err}
		}
		location := resp.Header.Get("Location")
		if location == "" {
			return nil, &upstreamHandshakeError{status: code, err: err}
		}
		base, e := url.Parse(uri)
		if e != nil {
			return nil, e
		}
		next, e := base.Parse(location)
		if e != nil {
			return nil, e
		}
		if next.Scheme == "https" {
			next.Scheme = "wss"
		} else if next.Scheme == "http" {
			next.Scheme = "ws"
		}
		uri = next.String()
	}
	return nil, fmt.Errorf("too many upstream redirects")
}

// injectRouterIntoVersionMessage adds "router" (EUI format) to a version message if missing, so the LNS can identify the gateway.
// Returns (modifiedJSON, true) when the message was a version message and router was injected, else (nil, false).
func injectRouterIntoVersionMessage(msg []byte, gatewayKey string) ([]byte, bool) {
	var m map[string]interface{}
	if err := json.Unmarshal(msg, &m); err != nil {
		return nil, false
	}
	if m["msgtype"] != "version" {
		return nil, false
	}
	if r, _ := m["router"].(string); r != "" {
		return nil, false
	}
	m["router"] = gatewayIDToEUIFormat(gatewayKey)
	out, err := json.Marshal(m)
	if err != nil {
		return nil, false
	}
	return out, true
}

// gatewayIDToEUIFormat returns a 16-char hex gateway ID as EUI-style with colons (e.g. b91a3108ac08317 -> b91a:3108:ac08:8317).
// Some LNS (e.g. ThingPark) expect this format in the URL or in the version message.
func gatewayIDToEUIFormat(gatewayID string) string {
	s := strings.ReplaceAll(gatewayID, ":", "")
	if len(s) != 16 {
		return gatewayID
	}
	var b strings.Builder
	for i := 0; i < 16; i += 4 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(s[i : i+4])
	}
	return b.String()
}

// appendRouterQuery appends ?router=gatewayID (or &router= if the URL already has a query) to the data URI.
// Some LNS (e.g. ThingPark) require the gateway identity in the URL; use EUI-style with colons when we only have normalized ID.
func appendRouterQuery(dataURI string, routerValue string) string {
	if routerValue == "" {
		return dataURI
	}
	u, err := url.Parse(dataURI)
	if err != nil {
		return dataURI
	}
	q := u.Query()
	q.Set("router", routerValue)
	u.RawQuery = q.Encode()
	return u.String()
}

// dataURI returns the single LNS data WebSocket URI: connect-data-uri if set, else connect-uri with optional connect-data-path.
func (p *WSProxyConfig) dataURI() (string, error) {
	if p.ConnectDataURI != "" {
		return strings.TrimSpace(p.ConnectDataURI), nil
	}
	base, err := url.Parse(p.ConnectURI)
	if err != nil {
		return "", fmt.Errorf("connect-uri: %w", err)
	}
	if p.ConnectDataPath != "" {
		path := p.ConnectDataPath
		if path[0] != '/' {
			path = "/" + path
		}
		base.Path = path
		base.RawPath = ""
	}
	return base.String(), nil
}

// dataURIForGateway returns the LNS data WebSocket URI for this gateway: stored value from router-info if present, otherwise config (connect-uri + connect-data-path).
func (p *WSProxyConfig) dataURIForGateway(gatewayID string) (string, error) {
	if p.CertStore != nil && gatewayID != "" {
		if uri, ok := p.CertStore.GetGatewayDataURI(gatewayID); ok {
			return uri, nil
		}
	}
	return p.dataURI()
}

// NewWSProxy creates a new WebSocket proxy.
func NewWSProxy(config *WSProxyConfig) (*WSProxy, error) {
	if config.ProxyMode == "" {
		config.ProxyMode = "local"
	}
	var registry *gatewayRegistry
	if config.ProxyMode == "shared" {
		var err error
		registry, err = loadGatewayRegistry(config.GatewayAuthFile)
		if err != nil {
			return nil, err
		}
	}

	if config.MaxStreams < 1 {
		return nil, fmt.Errorf("max streams must be positive")
	}
	sessions := &sessionRegistry{items: make(map[string]*session), capacity: config.MaxStreams}
	root, cancel := context.WithCancel(context.Background())
	if config.PingInterval <= 0 {
		config.PingInterval = 20 * time.Second
	}
	if config.PongTimeout <= 0 {
		config.PongTimeout = 60 * time.Second
	}
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	p := &WSProxy{
		config: config,
		routes: make(map[string]string), pending: make(map[*websocket.Conn]struct{}), admission: make(chan struct{}, config.MaxStreams), root: root, cancel: cancel,
		auth:       registry,
		upgrader:   upgrader,
		sessions:   sessions,
		httpClient: config.buildHTTPClient(nil),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/router-info", p.handleRouterInfo)
	mux.HandleFunc("/", p.handleDataConnection)
	p.server = &http.Server{
		Addr:         net.JoinHostPort(config.ListenHost, strconv.Itoa(config.ListenPort)),
		Handler:      mux,
		ReadTimeout:  httpServerTimeout,
		WriteTimeout: httpServerTimeout,
	}
	return p, nil
}

// Start starts the HTTP server (blocking).
func (p *WSProxy) Start() error {
	log.Infof("WebSocket proxy listening on %s", p.server.Addr)
	if p.config.ListenTLSCert != "" && p.config.ListenTLSKey != "" {
		cert, err := tls.LoadX509KeyPair(p.config.ListenTLSCert, p.config.ListenTLSKey)
		if err != nil {
			return fmt.Errorf("listen TLS: %w", err)
		}
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequestClientCert,
			MinVersion:   tls.VersionTLS12,
		}
		listener, err := tls.Listen("tcp", p.server.Addr, tlsConfig)
		if err != nil {
			return err
		}
		return p.server.Serve(listener)
	}
	return p.server.ListenAndServe()
}

// handleRouterInfo handles the discovery phase: forward request to LNS, rewrite uri in response to point to proxy.
func (p *WSProxy) handleRouterInfo(w http.ResponseWriter, r *http.Request) {
	if !p.admit(w) {
		return
	}
	defer p.release()
	gateway, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	p.track(gateway, true)
	defer func() { p.track(gateway, false); gateway.Close() }()
	gateway.SetReadLimit(maxMessageSize)
	_ = gateway.SetReadDeadline(time.Now().Add(wsHandshakeTimeout))
	_, raw, err := gateway.ReadMessage()
	if err != nil {
		return
	}
	var req RouterInfoRequest
	if err = json.Unmarshal(raw, &req); err != nil {
		_ = gateway.WriteJSON(map[string]string{"error": "invalid router identity"})
		return
	}
	id, err := p.authorize(r, req.Router)
	if err != nil {
		_ = gateway.WriteJSON(map[string]string{"error": "gateway authorization rejected"})
		return
	}
	discovery := p.config.ConnectDiscoveryURI
	if discovery == "" {
		base := p.config.ConnectURI
		if p.config.ConnectDataURI != "" {
			base = p.config.ConnectDataURI
		}
		u, e := url.Parse(base)
		if e != nil {
			return
		}
		path := p.config.ConnectDiscoveryPath
		if path == "" {
			path = "/router-info"
		}
		u.Path = "/" + strings.TrimPrefix(path, "/")
		u.RawPath = ""
		u.RawQuery = ""
		discovery = u.String()
	}
	dialer, err := p.config.dialer(p.config.clientCertForGatewayWebSocket(id))
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(p.root, wsHandshakeTimeout)
	defer cancel()
	upstream, err := p.config.dialContextWithRedirects(ctx, dialer, discovery, p.upstreamHeaders(id, "tc", r.Header))
	if err != nil {
		var handshake *upstreamHandshakeError
		if errors.As(err, &handshake) && handshake.status == http.StatusNotFound {
			p.setRoute(id, "")
			_ = gateway.WriteJSON(RouterInfoResponse{Router: gatewayIDToEUIFormat(id), URI: appendRouterQuery(p.publicURI(), id)})
		} else {
			_ = gateway.WriteJSON(map[string]string{"error": "upstream discovery failed"})
		}
		return
	}
	defer upstream.Close()
	_ = upstream.SetWriteDeadline(time.Now().Add(writeWait))
	_ = upstream.SetReadDeadline(time.Now().Add(wsHandshakeTimeout))
	upstream.SetReadLimit(maxMessageSize)
	if upstream.WriteMessage(websocket.TextMessage, raw) != nil {
		return
	}
	_, response, err := upstream.ReadMessage()
	if err != nil {
		return
	}
	var info RouterInfoResponse
	if json.Unmarshal(response, &info) != nil {
		return
	}
	if info.Error != "" {
		p.setRoute(id, "")
		_ = gateway.WriteMessage(websocket.TextMessage, response)
		return
	}
	if (info.Router != "" && NormalizeGatewayID(info.Router) != id) || info.URI == "" || !p.config.allowedUpstream(info.URI) {
		_ = gateway.WriteJSON(map[string]string{"error": "unapproved discovery endpoint"})
		return
	}
	p.setRoute(id, info.URI)
	info.Router = gatewayIDToEUIFormat(id)
	info.URI = appendRouterQuery(p.publicURI(), id)
	_ = gateway.WriteJSON(info)
}

// isWebSocketUpgrade returns true if the request asks for a WebSocket upgrade.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") &&
		strings.Contains(strings.ToLower(strings.TrimSpace(r.Header.Get("Connection"))), "upgrade")
}

// gatewayLogPrefix returns a consistent prefix for gateway-specific logs (EUI/router or "?" if unknown).
func gatewayLogPrefix(gatewayEUI string) string {
	if gatewayEUI == "" {
		return "[gateway ?] "
	}
	return "[gateway " + gatewayEUI + "] "
}

// cupsUpdateInfoBody is the JSON body for CUPS POST /update-info (doc.sm.tc/station/cupsproto).
// Field order matches the spec; some upstreams (e.g. ThingPark) may require this order.
type cupsUpdateInfoBody struct {
	Router      string  `json:"router"`
	CupsURI     string  `json:"cupsUri"`
	TcURI       *string `json:"tcUri"`
	CupsCredCrc *int64  `json:"cupsCredCrc,omitempty"`
	TcCredCrc   *int64  `json:"tcCredCrc,omitempty"`
	Station     string  `json:"station,omitempty"`
	Model       string  `json:"model,omitempty"`
	Package     *string `json:"package"` // present so upstream gets null when gateway sends null
	Keys        []int64 `json:"keys"`    // present so upstream gets [] when gateway sends []
}

// proxyCredCrcFromConnectTLSCA returns CRC32 of the connect-tls-ca file (proxy's cups.trust).
// Used when we have no server-issued cert: we send this so upstream sees the trust we use.
// Returns 0 if the file is missing or ConnectTLSInsecure (no CA to include).
func (p *WSProxyConfig) proxyCredCrcFromConnectTLSCA() int64 {
	if p.ConnectTLSCA == "" || p.ConnectTLSInsecure {
		return 0
	}
	b, err := os.ReadFile(p.ConnectTLSCA)
	if err != nil {
		return 0
	}
	return int64(crc32.ChecksumIEEE(b))
}

// trustDERFromConnectTLSCA returns the first certificate from the connect-tls-ca file as DER (for CUPS cred blob).
// Returns nil if the file is missing, unreadable, or has no CERTIFICATE block.
func (p *WSProxyConfig) trustDERFromConnectTLSCA() []byte {
	if p.ConnectTLSCA == "" {
		return nil
	}
	b, err := os.ReadFile(p.ConnectTLSCA)
	if err != nil {
		return nil
	}
	for len(b) > 0 {
		var block *pem.Block
		block, b = pem.Decode(b)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			return block.Bytes
		}
	}
	return nil
}

func cupsUpdateInfoFromRaw(raw map[string]interface{}, upstreamCupsURI string, proxyCredCrc *int64) cupsUpdateInfoBody {
	out := cupsUpdateInfoBody{CupsURI: upstreamCupsURI}
	if s, ok := raw["router"].(string); ok {
		out.Router = s
	}
	if v := raw["tcUri"]; v != nil {
		if s, ok := v.(string); ok && s != "" {
			out.TcURI = &s
		}
	}
	if proxyCredCrc != nil {
		out.CupsCredCrc = proxyCredCrc
		out.TcCredCrc = proxyCredCrc
	} else {
		if n, ok := toInt64(raw["cupsCredCrc"]); ok {
			out.CupsCredCrc = &n
		}
		if n, ok := toInt64(raw["tcCredCrc"]); ok {
			out.TcCredCrc = &n
		}
	}
	if s, ok := raw["station"].(string); ok {
		out.Station = s
	}
	if s, ok := raw["model"].(string); ok {
		out.Model = s
	}
	// Preserve package as null when gateway sends null; otherwise use string value.
	if v := raw["package"]; v == nil {
		out.Package = nil
	} else if s, ok := v.(string); ok {
		out.Package = &s
	}
	if arr, ok := raw["keys"].([]interface{}); ok {
		out.Keys = make([]int64, 0, len(arr)) // non-nil so JSON is [] not null
		for _, v := range arr {
			if n, ok := toInt64(v); ok {
				out.Keys = append(out.Keys, n)
			}
		}
	}
	return out
}

func ptrInt64(n int64) *int64 { return &n }

func toInt64(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case int:
		return int64(x), true
	case int64:
		return x, true
	default:
		return 0, false
	}
}

// handleHTTPProxy forwards non-WebSocket HTTP requests (e.g. CUPS update-info) to the upstream.
// For POST /update-info: uses server-issued cert for upstream if we have one (from a previous response);
// otherwise no client cert (or our issued cert if cups-upstream-client-cert). After a 200 response,
// parses the CUPS binary response and stores any server-issued credentials for this gateway for future use.
func (p *WSProxy) handleHTTPProxy(w http.ResponseWriter, r *http.Request) {
	if p.config.CUPSMode != "managed" || r.Method != http.MethodPost || r.URL.Path != "/update-info" {
		http.NotFound(w, r)
		return
	}

	connectURL, err := url.Parse(p.config.ConnectURI)
	if err != nil {
		http.Error(w, "bad upstream URI", http.StatusBadGateway)
		return
	}
	upstreamScheme := "https"
	if connectURL.Scheme == "ws" {
		upstreamScheme = "http"
	}
	upstreamURL := fmt.Sprintf("%s://%s%s", upstreamScheme, connectURL.Host, r.URL.RequestURI())
	log.Debugf("HTTP proxy: %s %s -> %s", r.Method, r.URL.RequestURI(), upstreamURL)
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var gatewayID string
	upstreamBody := body
	if r.URL.Path == "/update-info" && r.Method == http.MethodPost {
		var raw map[string]interface{}
		if jsonErr := json.Unmarshal(body, &raw); jsonErr == nil {
			if r, ok := raw["router"].(string); ok && r != "" {
				gatewayID = NormalizeGatewayID(r)
			}
			upstreamCupsURI := fmt.Sprintf("%s://%s", upstreamScheme, connectURL.Host)
			// The proxy→upstream (LNS/CUPS) connection always uses the given CA (connect-tls-ca).
			// Always send that CA's CRC so the upstream sees a consistent client; do not forward
			// the station's CRCs (they refer to proxy-facing creds and change after we send synthetic responses).
			var proxyCredCrc *int64
			if crc := p.config.proxyCredCrcFromConnectTLSCA(); crc != 0 {
				proxyCredCrc = &crc
			}
			cupsBody := cupsUpdateInfoFromRaw(raw, upstreamCupsURI, proxyCredCrc)
			if rewritten, marshalErr := json.Marshal(cupsBody); marshalErr == nil {
				upstreamBody = rewritten
				tcVal := "null"
				if cupsBody.TcURI != nil {
					tcVal = *cupsBody.TcURI
				}
				log.Debugf("%sCUPS request rewritten: cupsUri=%q tcUri=%s", gatewayLogPrefix(gatewayID), upstreamCupsURI, tcVal)
			}
		}
	}
	var authErr error
	gatewayID, authErr = p.authorize(r, gatewayID)
	if authErr != nil {
		http.Error(w, "gateway authorization rejected", http.StatusForbidden)
		return
	}
	httpPre := gatewayLogPrefix(gatewayID)
	if p.config.Verbose && len(body) > 0 {
		log.Info(Hexdump(httpPre+"HTTP incoming from gateway ", body, 2048))
	}

	var client *http.Client
	if p.config.CertStore != nil && gatewayID != "" {
		if serverCert, ok := p.config.CertStore.GetServerIssuedCert(gatewayID); ok {
			client = p.config.buildHTTPClient(serverCert)
			log.Debugf("%sCUPS upstream using server-issued client cert", httpPre)
		} else if p.config.CertsUpstreamClientCert {
			clientCert, certErr := p.config.CertStore.GetOrCreateClientCert(gatewayID)
			if certErr != nil {
				log.Warnf("%sCUPS cert: %v", httpPre, certErr)
			} else {
				client = p.config.buildHTTPClient(clientCert)
				log.Debugf("%sCUPS upstream using issued client cert", httpPre)
			}
		}
	}
	if client == nil {
		client = p.httpClient
		if r.URL.Path == "/update-info" && gatewayID != "" {
			log.Debugf("%sCUPS upstream using server auth only (no client cert)", httpPre)
		}
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.ContentLength = int64(len(upstreamBody))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(upstreamBody)), nil }
	for k, v := range r.Header {
		if len(v) == 0 {
			continue
		}
		switch strings.ToLower(k) {
		case "host", "connection", "upgrade", "sec-websocket-key", "sec-websocket-version", "sec-websocket-extensions", "sec-websocket-protocol":
			continue
		}
		req.Header[k] = v
	}
	req.Header.Del("Authorization")
	for k, v := range p.upstreamHeaders(gatewayID, "cups", r.Header) {
		req.Header[k] = v
	}
	if req.Header.Get("Content-Type") == "" && r.URL.Path == "/update-info" {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.config.Verbose {
		log.Infof("%sHTTP request to %s %s:", httpPre, req.Method, upstreamURL)
		log.Info(Hexdump(httpPre+"  request body ", upstreamBody, 2048))
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Warnf("HTTP proxy upstream %s: %v (check connect-tls-ca / connect-tls-insecure if TLS)", upstreamURL, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if p.config.Verbose {
		log.Infof("%sHTTP response from upstream: %d %s (body %d bytes)", httpPre, resp.StatusCode, resp.Status, len(respBody))
		if len(respBody) > 0 {
			log.Info(Hexdump(httpPre+"  response body ", respBody, 2048))
		}
	}

	// Intercept CUPS update-info 200 response: parse original, store server-issued creds from original only,
	// then send station a response with proxy URIs and proxy-issued creds (so station trusts proxy) while preserving sig/update data.
	if r.URL.Path == "/update-info" && r.Method == http.MethodPost && resp.StatusCode == 200 {
		if resp.Header.Get("Content-Type") == "application/octet-stream" && len(respBody) > 0 {
			proxyScheme := "https"
			proxyWSScheme := "wss"
			if r.TLS == nil {
				proxyScheme = "http"
				proxyWSScheme = "ws"
			}
			proxyCupsURI := proxyScheme + "://" + r.Host
			proxyTcURI := proxyWSScheme + "://" + r.Host

			// Parse the original upstream response once; we use it for storage and for building the reply.
			parsed, parseErr := ParseCUPSUpdateResponseFull(respBody)
			if parseErr != nil {
				log.Debugf("%sCUPS response parse: %v (passing through)", httpPre, parseErr)
			} else {
				// Store server-issued credentials from the original response only (never from our synthetic response).
				if p.config.CertStore != nil && gatewayID != "" {
					for name, cred := range map[string][]byte{"cups": parsed.CupsCred, "tc": parsed.TcCred} {
						if len(cred) == 0 {
							continue
						}
						certDER, keyDER, extractErr := extractCertKeyFromCredBlob(cred)
						if extractErr != nil || len(certDER) <= 4 || len(keyDER) == 0 {
							continue
						}
						suffix := gatewayID
						if name == "tc" {
							suffix = gatewayID + "_tc"
						}
						if putErr := p.config.CertStore.PutServerIssuedCert(suffix, certDER, keyDER); putErr != nil {
							log.Warnf("Store %s server-issued cert for gateway %s: %v", name, gatewayID, putErr)
						}
					}
				}

				if IsCupsNullResponse(respBody) {
					// Null response: synthesize reply with proxy URIs and our-issued cert so station trusts the proxy.
					if p.config.CertStore == nil || gatewayID == "" {
						if gatewayID == "" && p.config.CertStore != nil {
							log.Warnf("%sCUPS null from upstream but gateway ID missing in request (no \"router\" in body); cannot send synthetic response - station needs router for tc.trust/cups.trust", httpPre)
						}
						log.Debugf("CUPS null response (no update), passing through unchanged")
					} else {
						ourCert, ourCertErr := p.config.CertStore.GetOrCreateClientCert(gatewayID)
						trustDER := p.config.CertStore.GetCACertDER()
						if ourCertErr == nil && ourCert != nil && len(ourCert.Certificate) > 0 && ourCert.PrivateKey != nil && trustDER != nil {
							keyDER, keyErr := KeyDERForCupsBlob(ourCert.PrivateKey)
							if keyErr == nil {
								credBlob := CredBlobFromTrustCertKey(trustDER, ourCert.Certificate[0], keyDER)
								if len(credBlob) > 0 {
									if built, buildErr := BuildCupsResponseWithCreds(proxyCupsURI, proxyTcURI, credBlob, credBlob); buildErr == nil {
										respBody = built
										log.Infof("%sCUPS null from upstream; responding with proxy URIs and our-issued cert (cups+tc cred %d bytes each) so gateway can install and verify proxy TLS", httpPre, len(credBlob))
									}
								}
							}
						} else {
							var tlsCert *tls.Certificate
							if c, ok := p.config.CertStore.GetServerIssuedCert(gatewayID); ok {
								tlsCert = c
							} else if c, ok := p.config.CertStore.GetServerIssuedCert(gatewayID + "_tc"); ok {
								tlsCert = c
							}
							if tlsCert != nil && len(tlsCert.Certificate) > 0 && tlsCert.PrivateKey != nil {
								trustDER := p.config.trustDERFromConnectTLSCA()
								keyDER, keyErr := KeyDERForCupsBlob(tlsCert.PrivateKey)
								if trustDER != nil && keyErr == nil {
									credBlob := CredBlobFromTrustCertKey(trustDER, tlsCert.Certificate[0], keyDER)
									if built, buildErr := BuildCupsResponseWithCreds(proxyCupsURI, proxyTcURI, credBlob, credBlob); buildErr == nil {
										respBody = built
										log.Infof("%sCUPS null from upstream; responding with proxy URIs and stored LNS cert (gateway may fail TLS verify against proxy)", httpPre)
									}
								}
							}
						}
						if IsCupsNullResponse(respBody) {
							log.Debugf("CUPS null response (no update), passing through unchanged")
						}
					}
				} else {
					// Non-null response: replace URIs and credentials with proxy's so station trusts proxy; preserve sig/update data.
					replaced := false
					if p.config.CertStore != nil && gatewayID != "" {
						ourCert, ourCertErr := p.config.CertStore.GetOrCreateClientCert(gatewayID)
						trustDER := p.config.CertStore.GetCACertDER()
						if ourCertErr == nil && ourCert != nil && len(ourCert.Certificate) > 0 && ourCert.PrivateKey != nil && trustDER != nil {
							keyDER, keyErr := KeyDERForCupsBlob(ourCert.PrivateKey)
							if keyErr == nil {
								credBlob := CredBlobFromTrustCertKey(trustDER, ourCert.Certificate[0], keyDER)
								if built, buildErr := BuildCupsResponseFull(proxyCupsURI, proxyTcURI, credBlob, credBlob, parsed.Trailing); buildErr == nil {
									respBody = built
									replaced = true
									log.Debugf("%sCUPS response rewritten: proxy URIs and proxy creds, trailing sig/update preserved", httpPre)
								}
							}
						}
					}
					if !replaced {
						// Fallback: only rewrite URIs (station may fail TLS until next null/synthetic).
						if rewritten, rewriteErr := RewriteCupsResponseURIs(respBody, proxyCupsURI, proxyTcURI); rewriteErr == nil {
							respBody = rewritten
							log.Debugf("CUPS response rewritten: cupsUri=%q tcUri=%q", proxyCupsURI, proxyTcURI)
						}
					}
				}
			}
		}
	}

	if resp.StatusCode >= 400 {
		log.Warnf("%sHTTP proxy upstream %s responded %d %s", httpPre, upstreamURL, resp.StatusCode, resp.Status)
		if len(respBody) > 0 {
			preview := respBody
			if len(preview) > 500 {
				preview = preview[:500]
			}
			log.Warnf("%sUpstream response body: %s", httpPre, string(preview))
			if p.config.Verbose {
				log.Debug(Hexdump(httpPre+"HTTP error response from upstream ", respBody, 2048))
			}
		}
	}
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	// Ensure Content-Length matches the body we actually send (we may have rewritten or synthesized it).
	w.Header().Set("Content-Length", strconv.Itoa(len(respBody)))
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(respBody); err != nil {
		log.Debugf("CUPS response write to gateway: %v", err)
	}
}

// gatewayIDFromDataRequest returns the gateway identity for a data WebSocket request.
// Uses only stable identifiers: "router" query param or TLS client cert Subject.CommonName.
// Does not use RemoteAddr, so cert/config lookups are keyed by LNS-allocated or cert identity, not endpoint.
func gatewayIDFromDataRequest(r *http.Request) string {
	if key, ok := r.URL.Query()["router"]; ok && len(key) > 0 {
		return NormalizeGatewayID(key[0])
	}
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		if cn := r.TLS.PeerCertificates[0].Subject.CommonName; cn != "" {
			return NormalizeGatewayID(cn)
		}
	}
	return ""
}

// handleDataConnection upgrades to WebSocket and bridges to upstream LNS, or proxies HTTP (e.g. CUPS).
func (p *WSProxy) handleDataConnection(w http.ResponseWriter, r *http.Request) {
	if !isWebSocketUpgrade(r) {
		p.handleHTTPProxy(w, r)
		return
	}
	id, err := p.authorize(r, r.URL.Query().Get("router"))
	if err != nil {
		http.Error(w, "gateway authorization rejected", http.StatusForbidden)
		return
	}
	if !p.admit(w) {
		return
	}
	released := false
	defer func() {
		if !released {
			p.release()
		}
	}()
	uri, err := p.config.dataURI()
	if err != nil {
		http.Error(w, "invalid upstream", 502)
		return
	}
	p.routesMu.Lock()
	if assigned := p.routes[id]; assigned != "" {
		uri = assigned
	}
	p.routesMu.Unlock()
	uri = appendRouterQuery(uri, gatewayIDToEUIFormat(id))
	dialer, err := p.config.dialerFor(p.config.clientCertForGatewayWebSocket(id), true)
	if err != nil {
		http.Error(w, "upstream TLS configuration failed", 502)
		return
	}
	if p.config.ConnectWSSubprotocol != "" {
		dialer.Subprotocols = []string{p.config.ConnectWSSubprotocol}
	} else {
		dialer.Subprotocols = websocket.Subprotocols(r)
	}
	ctx, cancel := context.WithTimeout(p.root, wsHandshakeTimeout)
	defer cancel()
	upstream, err := p.config.dialContextWithRedirects(ctx, dialer, uri, p.upstreamHeaders(id, "tc", r.Header))
	if err != nil {
		http.Error(w, "upstream handshake failed", 502)
		return
	}
	upgrader := p.upgrader
	for _, offered := range websocket.Subprotocols(r) {
		if offered == upstream.Subprotocol() {
			upgrader.Subprotocols = []string{offered}
			break
		}
	}
	gateway, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		upstream.Close()
		return
	}
	connection := &session{gatewayKey: id, gatewayConn: gateway, upstreamConn: upstream, config: p.config, events: p.config.Events, done: make(chan struct{})}
	if !p.sessions.Add(id, connection) {
		connection.close()
		return
	}
	// Admission limits pending handshakes, not the lifetime of established sessions.
	p.release()
	released = true
	go connection.upstreamPingLoop()
	go connection.gatewayToUpstream()
	go connection.upstreamToGateway()
	<-connection.done
	if p.sessions.RemoveCurrent(id, connection) {
		if events, ok := p.config.Events.(interface{ OnSessionClosed(string) }); ok {
			events.OnSessionClosed(id)
		}
	}
}

func (s *session) close() {
	s.closeOnce.Do(func() {
		if s.gatewayConn != nil {
			_ = s.gatewayConn.Close()
		}
		if s.upstreamConn != nil {
			_ = s.upstreamConn.Close()
		}
		close(s.done)
	})
}

// upstreamPingLoop sends WebSocket ping frames to the LNS periodically so the server (or load balancer) keeps the connection active. Delay the first ping so the version message is sent first; some LNS (e.g. ThingPark) may close the connection if they receive a ping before the expected first application message.
func (s *session) upstreamPingLoop() {
	ticker := time.NewTicker(s.config.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			for _, c := range []*websocket.Conn{s.gatewayConn, s.upstreamConn} {
				if c.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)) != nil {
					s.close()
					return
				}
			}
		}
	}
}

func (s *session) gatewayToUpstream() {
	defer s.close()
	s.gatewayConn.SetReadLimit(maxMessageSize)
	s.gatewayConn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
	s.gatewayConn.SetPongHandler(func(string) error {
		s.gatewayConn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
		return nil
	})
	for {
		mt, msg, err := s.gatewayConn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Debugf("%sgateway read: %v", gatewayLogPrefix(s.gatewayKey), err)
			}
			break
		}
		if s.config.DebugDump != nil {
			s.config.DebugDump.Write("up", s.gatewayKey, msg, mt == websocket.BinaryMessage)
		}
		if s.config.Verbose && len(msg) > 0 {
			dir := "text"
			if mt == websocket.BinaryMessage {
				dir = "binary"
			}
			log.Debug(Hexdump(gatewayLogPrefix(s.gatewayKey)+"WS payload sent to LNS "+dir+" ", msg, 512))
		}
		// Some LNS (e.g. ThingPark) require the gateway identity in the first (version) message; inject "router" if missing.
		toSend := msg
		if s.config.InjectRouterIntoVersion && mt == websocket.TextMessage && len(msg) > 0 {
			if injected, ok := injectRouterIntoVersionMessage(msg, s.gatewayKey); ok {
				toSend = injected
			}
		}
		// Forward first so the tunnel is never blocked or broken by analytics logic.
		s.upstreamConn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := s.upstreamConn.WriteMessage(mt, toSend); err != nil {
			log.Warnf("%supstream write: %v", gatewayLogPrefix(s.gatewayKey), err)
			break
		}
		if s.events != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Warnf("%sOnUpstreamMessage panic (tunnel unaffected): %v", gatewayLogPrefix(s.gatewayKey), r)
					}
				}()
				s.events.OnUpstreamMessage(s.gatewayKey, msg, mt == websocket.BinaryMessage)
			}()
		}
	}
}

func (s *session) upstreamToGateway() {
	defer s.close()
	s.upstreamConn.SetReadLimit(maxMessageSize)
	s.upstreamConn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
	s.upstreamConn.SetPongHandler(func(string) error {
		s.upstreamConn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
		return nil
	})
	for {
		mt, msg, err := s.upstreamConn.ReadMessage()
		if err != nil {
			if ce, ok := err.(*websocket.CloseError); ok {
				log.Warnf("%sLNS closed connection: code=%d reason=%q (gateway will be disconnected)", gatewayLogPrefix(s.gatewayKey), ce.Code, ce.Text)
				// Forward close to gateway so the station sees a clean close with LNS code/reason
				code := ce.Code
				if code == 1005 || code == 1006 || code == 1015 {
					code = websocket.CloseGoingAway
				}
				payload := websocket.FormatCloseMessage(code, ce.Text)
				_ = s.gatewayConn.WriteControl(websocket.CloseMessage, payload, time.Now().Add(writeWait))
			} else if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Warnf("%supstream read (LNS closed or error): %v", gatewayLogPrefix(s.gatewayKey), err)
			} else {
				log.Debugf("%supstream read closed: %v", gatewayLogPrefix(s.gatewayKey), err)
			}
			break
		}
		if s.config.DebugDump != nil {
			s.config.DebugDump.Write("dn", s.gatewayKey, msg, mt == websocket.BinaryMessage)
		}
		if s.config.Verbose && len(msg) > 0 {
			dir := "text"
			if mt == websocket.BinaryMessage {
				dir = "binary"
			}
			log.Debug(Hexdump(gatewayLogPrefix(s.gatewayKey)+"WS reply from LNS "+dir+" ", msg, 512))
		}
		// Forward first so the tunnel is never blocked or broken by analytics logic.
		s.gatewayConn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := s.gatewayConn.WriteMessage(mt, msg); err != nil {
			log.Warnf("%sgateway write: %v", gatewayLogPrefix(s.gatewayKey), err)
			break
		}
		if s.events != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Warnf("%sOnDownstreamMessage panic (tunnel unaffected): %v", gatewayLogPrefix(s.gatewayKey), r)
					}
				}()
				s.events.OnDownstreamMessage(s.gatewayKey, msg, mt == websocket.BinaryMessage)
			}()
		}
	}
}

// DebugDumpWriter writes traffic for debugging.
type DebugDumpWriter struct {
	w  io.Writer
	mu sync.Mutex
}

// NewDebugDumpWriter creates a debug dump writer.
func NewDebugDumpWriter(w io.Writer) *DebugDumpWriter {
	return &DebugDumpWriter{w: w}
}

// Write writes a line: direction:key:base64payload:binary
func (d *DebugDumpWriter) Write(direction, key string, payload []byte, binary bool) {
	if d == nil || d.w == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	b := bytes.Buffer{}
	b.WriteString(direction)
	b.WriteString(":")
	b.WriteString(key)
	b.WriteString(":")
	b.WriteString(string(payload))
	if binary {
		b.WriteString(":bin")
	}
	b.WriteString("\n")
	_, _ = d.w.Write(b.Bytes())
}

// Shutdown closes hijacked WebSockets as well as ordinary HTTP connections.
func (p *WSProxy) Shutdown(ctx context.Context) error {
	p.cancel()
	p.pendingMu.Lock()
	for c := range p.pending {
		c.Close()
	}
	p.pendingMu.Unlock()
	p.sessions.CloseAll()
	return p.server.Shutdown(ctx)
}
