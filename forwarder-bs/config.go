package forwarderbs

import (
	"bufio"

	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/kudzutechnologies/analytics/client"
	log "github.com/sirupsen/logrus"
)

type ForwarderConfig struct {
	Logger                log.FieldLogger        `json:"-"`
	GatewayAuthorizations []GatewayAuthorization `json:"-"`
	IssuerCert            string                 `json:"issuer-cert,omitempty"`
	IssuerKey             string                 `json:"issuer-key,omitempty"`
	BootstrapGW           string                 `json:"bootstrap-gw,omitempty"`
	BootstrapDir          string                 `json:"bootstrap-dir,omitempty"`
	CertsCACertPEM        []byte                 `json:"-"`
	CertsCAKeyPEM         []byte                 `json:"-"`
	IssuerCertPEM         []byte                 `json:"-"`
	IssuerKeyPEM          []byte                 `json:"-"`
	ListenTLSCertPEM      []byte                 `json:"-"`
	ListenTLSKeyPEM       []byte                 `json:"-"`
	ConnectTLSCAPEM       []byte                 `json:"-"`
	ConnectDataTLSCAPEM   []byte                 `json:"-"`

	// WebSocket proxy
	ListenHost               string `json:"listen-host,omitempty"`
	ListenPort               int    `json:"listen-port,omitempty"`
	ConnectURI               string `json:"connect-uri,omitempty"`
	ConnectDiscoveryURI      string `json:"connect-discovery-uri,omitempty"`  // optional; full router-info WebSocket URI (overrides path)
	ConnectDiscoveryPath     string `json:"connect-discovery-path,omitempty"` // optional; router-info path on connect-uri host (e.g. /lns/router-info); default /router-info
	ConnectDataPath          string `json:"connect-data-path,omitempty"`      // optional; path for LNS data WebSocket on connect-uri host (e.g. /tpe/lns when root returns HTML)
	ConnectDataURI           string `json:"connect-data-uri,omitempty"`       // optional; full LNS data WebSocket URI (overrides connect-uri + connect-data-path when set)
	ConnectWSSubprotocol     string `json:"connect-ws-subprotocol,omitempty"`
	ListenTLSCert            string `json:"listen-tls-cert,omitempty"`
	ListenTLSKey             string `json:"listen-tls-key,omitempty"`
	ConnectTLSCA             string `json:"connect-tls-ca,omitempty"`
	ConnectTLSInsecure       bool   `json:"connect-tls-insecure,omitempty"`
	ConnectDataTLSServerName string `json:"connect-data-tls-server-name,omitempty"` // override TLS ServerName for data connection when server cert hostname differs (e.g. lns-tpw.thingpark.com)
	ConnectDataTLSCA         string `json:"connect-data-tls-ca,omitempty"`          // optional; CA file for data connection TLS when LNS uses a different CA than connect-uri
	ConnectDataTLSInsecure   bool   `json:"connect-data-tls-insecure,omitempty"`    // skip server cert verification for data connection only (client cert still sent)
	CertsDir                 string `json:"certs-dir,omitempty"`
	CertsCACert              string `json:"certs-ca-cert,omitempty"`
	CertsCAKey               string `json:"certs-ca-key,omitempty"`
	CertsUpstreamClientCert  bool   `json:"certs-upstream-client-cert,omitempty"` // if true, use proxy-issued client cert for CUPS when no server-issued cert; only valid when upstream trusts proxy CA (e.g. your own LNS)

	// Analytics client
	ClientId            string `json:"client-id,omitempty"`
	ClientKey           string `json:"client-key,omitempty"`
	Endpoint            string `json:"analytics-endpoint,omitempty"`
	PairingEndpoint     string `json:"pairing-endpoint,omitempty"`
	CAFile              string `json:"analytics-ca-file,omitempty"`
	ConnectTimeout      int    `json:"analytics-connect-timeout,omitempty"`
	RequestTimeout      int    `json:"analytics-request-timeout,omitempty"`
	MaxReconnectBackoff int    `json:"analytics-max-backoff,omitempty"`

	// Forwarder behavior
	FlushInterval int    `json:"batch-sec,omitempty"`
	GatewayId     string `json:"gateway,omitempty"`
	GaugeStat     bool   `json:"gauge-stat,omitempty"`
	ServerSide    bool   `json:"server-side,omitempty"`
	MaxWSStreams  int    `json:"max-ws-streams,omitempty"`

	GatewayEUI              string `json:"gateway-eui,omitempty"`
	CUPSMaxCredentialBytes  int    `json:"cups-max-credential-bytes,omitempty"`
	MaxBatches              int    `json:"max-batches,omitempty"`
	MaxMessages             int    `json:"max-messages,omitempty"`
	MaxBufferBytes          int    `json:"max-buffer-bytes,omitempty"`
	PreIdentityMaxMessages  int    `json:"pre-identity-max-messages,omitempty"`
	ShutdownDeadlineMS      int    `json:"shutdown-deadline-ms,omitempty"`
	ProxyMode               string `json:"proxy-mode,omitempty"`
	PublicURI               string `json:"public-uri,omitempty"`
	GatewayAuthFile         string `json:"gateway-auth-file,omitempty"`
	UpstreamAllowedOrigins  string `json:"upstream-allowed-origins,omitempty"`
	CUPSMode                string `json:"cups-mode,omitempty"`
	InjectRouterIntoVersion bool   `json:"inject-router-into-version,omitempty"`
	ConfigPath              string `json:"-"`
	PairPin                 string `json:"-"`
	WriteConfig             bool   `json:"-"`
	ShowVersion             bool   `json:"-"`
	LogFile                 string `json:"log-file,omitempty"`
	// Debug
	DebugDump         string `json:"debug-dump,omitempty"`
	DebugDumpMaxBytes int    `json:"debug-dump-max-bytes,omitempty"`
	LogLevel          string `json:"log-level,omitempty"`
	Verbose           bool   `json:"verbose,omitempty"` // enable debug log level + hexdump of incoming/outgoing traffic
}

var defaultConf = ForwarderConfig{
	ListenHost: "127.0.0.1", ListenPort: 8090,
	Endpoint: client.DefaultEndpoint, PairingEndpoint: client.DefaultPairingEndpoint,
	ConnectTimeout: 5, RequestTimeout: 5, MaxReconnectBackoff: 60,
	FlushInterval: 10, CUPSMaxCredentialBytes: 16384, MaxBatches: 2, MaxMessages: 32, MaxBufferBytes: 65536,
	ShutdownDeadlineMS: 5000, ProxyMode: "local", CUPSMode: "off", LogLevel: "info", DebugDumpMaxBytes: 1048576,
}

func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return "unknown"
}

var configAliases = map[string]string{"flush-interval": "batch-sec", "endpoint": "analytics-endpoint", "ca-file": "analytics-ca-file"}

func canonicalOption(k string) string {
	if v, ok := configAliases[k]; ok {
		return v
	}
	return k
}

// ParseForwarderConfig has no global flags or process side effects. Tests and
// pairing use exactly the same precedence and validation as the daemon.
func ParseForwarderConfig(args, environ []string) (ForwarderConfig, error) {
	c := defaultConf
	fields := map[string]reflect.Value{}
	rv := reflect.ValueOf(&c).Elem()
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		k := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if k != "-" && k != "" {
			fields[k] = rv.Field(i)
		}
	}
	fields["config"] = reflect.ValueOf(&c.ConfigPath).Elem()
	fields["pair-pin"] = reflect.ValueOf(&c.PairPin).Elem()
	fields["write"] = reflect.ValueOf(&c.WriteConfig).Elem()
	fields["version"] = reflect.ValueOf(&c.ShowVersion).Elem()
	set := func(k, v string) error {
		k = canonicalOption(k)
		f, ok := fields[k]
		if !ok {
			return fmt.Errorf("unknown option %q", k)
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString(v)
		case reflect.Int:
			n, e := strconv.ParseInt(v, 10, 32)
			if e != nil {
				return fmt.Errorf("%s: invalid integer %q", k, v)
			}
			f.SetInt(n)
		case reflect.Bool:
			b, e := strconv.ParseBool(v)
			if e != nil {
				return fmt.Errorf("%s: invalid boolean %q", k, v)
			}
			f.SetBool(b)
		}
		return nil
	}
	env := map[string]string{}
	for _, entry := range environ {
		if kv := strings.SplitN(entry, "=", 2); len(kv) == 2 {
			env[kv[0]] = kv[1]
		}
	}
	c.ConfigPath = env["CONFIG"]
	for i, a := range args {
		a = strings.TrimLeft(a, "-")
		kv := strings.SplitN(a, "=", 2)
		if kv[0] == "config" {
			if len(kv) == 2 {
				c.ConfigPath = kv[1]
			} else if i+1 < len(args) {
				c.ConfigPath = args[i+1]
			}
		}
	}
	// A new pairing destination may not exist yet. Other explicit file failures
	// are errors, including directories and permission failures.
	if c.ConfigPath != "" {
		f, e := os.Open(c.ConfigPath)
		newPairing := false
		for _, a := range args {
			if strings.HasPrefix(a, "--pair-pin") || strings.HasPrefix(a, "-pair-pin") {
				newPairing = true
			}
		}
		if e != nil {
			if !(os.IsNotExist(e) && newPairing) {
				return c, fmt.Errorf("config: %w", e)
			}
		} else {
			defer f.Close()
			scan := bufio.NewScanner(f)
			seen := map[string]string{}
			for scan.Scan() {
				line := strings.TrimSpace(scan.Text())
				if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
					continue
				}
				kv := strings.SplitN(line, "=", 2)
				if len(kv) != 2 {
					return c, fmt.Errorf("config: expected key=value")
				}
				k, v := strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
				if len(v) > 0 && (v[0] == '"' || v[0] == '\'') {
					if len(v) < 2 || v[len(v)-1] != v[0] {
						return c, fmt.Errorf("config: unclosed quote")
					}
					v = v[1 : len(v)-1]
				}
				canonical := canonicalOption(k)
				if old, ok := seen[canonical]; ok && old != v {
					return c, fmt.Errorf("conflicting config aliases for %s", canonical)
				}
				seen[canonical] = v
				if e := set(k, v); e != nil {
					return c, e
				}
			}
			if e := scan.Err(); e != nil {
				return c, e
			}
		}
	}
	seenEnv := map[string]string{}
	for k := range fields {
		ek := strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
		if v, ok := env[ek]; ok {
			seenEnv[k] = v
			if e := set(k, v); e != nil {
				return c, e
			}
		}
	}
	for alias, canonical := range configAliases {
		ek := strings.ToUpper(strings.ReplaceAll(alias, "-", "_"))
		if v, ok := env[ek]; ok {
			if old, exists := seenEnv[canonical]; exists && old != v {
				return c, fmt.Errorf("conflicting environment aliases for %s", canonical)
			}
			if e := set(canonical, v); e != nil {
				return c, e
			}
		}
	}
	fs := flag.NewFlagSet("kudzu-forwarder-bs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for k, f := range fields {
		switch f.Kind() {
		case reflect.String:
			fs.StringVar(f.Addr().Interface().(*string), k, f.String(), k)
		case reflect.Int:
			fs.IntVar(f.Addr().Interface().(*int), k, int(f.Int()), k)
		case reflect.Bool:
			fs.BoolVar(f.Addr().Interface().(*bool), k, f.Bool(), k)
		}
	}
	normalized := make([]string, 0, len(args))
	seenCLI := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return c, fmt.Errorf("unexpected argument %q", a)
		}
		kv := strings.SplitN(strings.TrimLeft(a, "-"), "=", 2)
		k := canonicalOption(kv[0])
		f, ok := fields[k]
		if !ok {
			if k == "h" || k == "help" {
				return c, flag.ErrHelp
			}
			return c, fmt.Errorf("unknown option %q", k)
		}
		v := "true"
		if len(kv) == 2 {
			v = kv[1]
		} else if f.Kind() != reflect.Bool {
			i++
			if i >= len(args) {
				return c, fmt.Errorf("missing value for %s", k)
			}
			v = args[i]
		}
		if old, exists := seenCLI[k]; exists && old != v {
			return c, fmt.Errorf("conflicting CLI values for %s", k)
		}
		seenCLI[k] = v
		normalized = append(normalized, "--"+k+"="+v)
	}
	if e := fs.Parse(normalized); e != nil {
		return c, e
	}
	if c.ShowVersion || c.PairPin != "" || c.BootstrapGW != "" {
		return c, nil
	}
	return c, ValidateForwarderConfig(&c)
}

func ValidateForwarderConfig(c *ForwarderConfig) error {
	rv := reflect.ValueOf(c).Elem()
	for i := 0; i < rv.NumField(); i++ {
		if rv.Field(i).Kind() == reflect.Int && rv.Field(i).Int() > 2147483647 {
			return fmt.Errorf("integer option exceeds portable 32-bit range")
		}
	}

	if c.ProxyMode != "local" && c.ProxyMode != "shared" {
		return fmt.Errorf("proxy-mode must be local or shared")
	}
	if c.MaxWSStreams == 0 {
		c.MaxWSStreams = 1
		if c.ProxyMode == "shared" {
			c.MaxWSStreams = 256
		}
	}
	for k, v := range map[string]int{"max-batches": c.MaxBatches, "max-messages": c.MaxMessages, "batch-sec": c.FlushInterval, "max-ws-streams": c.MaxWSStreams, "analytics-connect-timeout": c.ConnectTimeout, "analytics-request-timeout": c.RequestTimeout, "analytics-max-backoff": c.MaxReconnectBackoff} {
		if v < 1 {
			return fmt.Errorf("%s must be at least 1", k)
		}
	}
	if c.CUPSMaxCredentialBytes < 1024 || c.CUPSMaxCredentialBytes > 65535 {
		return fmt.Errorf("invalid CUPS credential limit")
	}
	if c.MaxBufferBytes < 1024 || c.ShutdownDeadlineMS < 0 || c.PreIdentityMaxMessages < 0 || c.DebugDumpMaxBytes < 0 {
		return fmt.Errorf("invalid buffer or shutdown limits")
	}
	if c.ListenPort < 0 || c.ListenPort > 65535 {
		return fmt.Errorf("invalid listen-port")
	}
	if c.ClientId == "" || c.ClientKey == "" || (!c.ServerSide && c.GatewayId == "") {
		return fmt.Errorf("client-id, client-key and client-side gateway are required")
	}
	for _, raw := range []string{c.ConnectURI, c.ConnectDataURI, c.ConnectDiscoveryURI} {
		if raw == "" {
			continue
		}
		u, e := url.Parse(raw)
		if e != nil || u.Host == "" || (u.Scheme != "ws" && u.Scheme != "wss") || u.User != nil {
			return fmt.Errorf("invalid upstream WebSocket URI")
		}
	}
	if c.ConnectURI == "" {
		return fmt.Errorf("connect-uri is required")
	}
	if (c.ListenTLSCert == "") != (c.ListenTLSKey == "") {
		return fmt.Errorf("listen TLS certificate and key must be configured together")
	}
	if c.ProxyMode == "local" {
		ip := net.ParseIP(c.ListenHost)
		if c.ListenHost != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("local proxy must bind to loopback")
		}
	}
	if c.ProxyMode == "shared" && (c.ListenTLSCert == "" || c.GatewayAuthFile == "" || c.PublicURI == "") {
		return fmt.Errorf("shared proxy requires TLS, public-uri and gateway-auth-file")
	}
	if c.CUPSMode != "off" && c.CUPSMode != "managed" {
		return fmt.Errorf("cups-mode must be off or managed")
	}
	if c.CUPSMode == "managed" && (c.CertsDir == "" || c.ListenTLSCert == "") {
		return fmt.Errorf("managed CUPS requires explicit TLS and certs-dir")
	}
	if c.PublicURI != "" {
		u, e := url.Parse(c.PublicURI)
		if e != nil || u.Host == "" || (u.Scheme != "ws" && u.Scheme != "wss") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("invalid public-uri")
		}
		if c.ProxyMode == "shared" && u.Scheme != "wss" {
			return fmt.Errorf("shared public-uri must use wss")
		}
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unknown log-level %q", c.LogLevel)
	}
	return nil
}

func ParseConfigFromEnv() (ForwarderConfig, error) {
	return ParseForwarderConfig(os.Args[1:], os.Environ())
}

func DefaultForwarderConfig() ForwarderConfig { return defaultConf }
