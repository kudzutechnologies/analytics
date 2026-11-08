package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/kudzutechnologies/analytics/client"
	"github.com/namsral/flag"
	log "github.com/sirupsen/logrus"
)

const MajorVersion = "0.1.0"

type ForwarderConfig struct {
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
	FlushInterval int    `json:"flush-interval,omitempty"`
	GatewayId     string `json:"gateway,omitempty"`
	GaugeStat     bool   `json:"gauge-stat,omitempty"`
	ServerSide    bool   `json:"server-side,omitempty"`
	MaxWSStreams  int    `json:"max-ws-streams,omitempty"`

	// Debug
	DebugDump string `json:"debug-dump,omitempty"`
	LogLevel  string `json:"log-level,omitempty"`
	Verbose   bool   `json:"verbose,omitempty"` // enable debug log level + hexdump of incoming/outgoing traffic
}

var defaultConf = ForwarderConfig{
	ListenHost:               "0.0.0.0",
	ListenPort:               8090,
	ConnectURI:               "",
	ConnectDiscoveryURI:      "",
	ConnectDiscoveryPath:     "",
	ConnectDataPath:          "",
	ConnectDataURI:           "",
	ListenTLSCert:            "",
	ListenTLSKey:             "",
	ConnectTLSCA:             "",
	ConnectTLSInsecure:       false,
	ConnectDataTLSServerName: "",
	ConnectDataTLSCA:         "",
	ConnectDataTLSInsecure:   false,
	CertsDir:                 "./certs",
	CertsCACert:              "",
	CertsCAKey:               "",
	CertsUpstreamClientCert:  false,
	ClientId:                 "",
	ClientKey:                "",
	Endpoint:                 client.DefaultEndpoint,
	PairingEndpoint:          client.DefaultPairingEndpoint,
	CAFile:                   "",
	ConnectTimeout:           0,
	RequestTimeout:           0,
	MaxReconnectBackoff:      0,
	FlushInterval:            0,
	GatewayId:                "",
	GaugeStat:                false,
	ServerSide:               false,
	MaxWSStreams:             0,
	DebugDump:                "",
	LogLevel:                 "info",
	Verbose:                  false,
}

func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return "unknown"
}

func ParseConfigFromEnv() ForwarderConfig {
	var config ForwarderConfig
	flag.String(flag.DefaultConfigFlagname, "", "path to the configuration file")

	// WebSocket proxy config
	flag.StringVar(&config.ListenHost, "listen-host", defaultConf.ListenHost, "local bind address for WebSocket server")
	flag.IntVar(&config.ListenPort, "listen-port", defaultConf.ListenPort, "local port for WebSocket server")
	flag.StringVar(&config.ConnectURI, "connect-uri", defaultConf.ConnectURI, "upstream LNS WebSocket URI for data (e.g. wss://lns.example.com:8887)")
	flag.StringVar(&config.ConnectDiscoveryURI, "connect-discovery-uri", defaultConf.ConnectDiscoveryURI, "optional full router-info WebSocket URI (overrides host/port/path)")
	flag.StringVar(&config.ConnectDiscoveryPath, "connect-discovery-path", defaultConf.ConnectDiscoveryPath, "optional router-info path on connect-uri host (default /router-info); use when LNS is on same port, different path")
	flag.StringVar(&config.ConnectDataPath, "connect-data-path", defaultConf.ConnectDataPath, "path for LNS data WebSocket on connect-uri host (e.g. /tpe/lns for ThingPark); when empty, data URI is connect-uri as-is")
	flag.StringVar(&config.ConnectDataURI, "connect-data-uri", defaultConf.ConnectDataURI, "optional full LNS data WebSocket URI (overrides connect-uri + connect-data-path); use when LNS is on a different host/port/path")
	flag.StringVar(&config.ConnectWSSubprotocol, "connect-ws-subprotocol", defaultConf.ConnectWSSubprotocol, "optional WebSocket subprotocol for upstream (e.g. lora.v2 for some LNS)")
	flag.StringVar(&config.ListenTLSCert, "listen-tls-cert", defaultConf.ListenTLSCert, "TLS certificate file for incoming gateway connections")
	flag.StringVar(&config.ListenTLSKey, "listen-tls-key", defaultConf.ListenTLSKey, "TLS key file for incoming gateway connections")
	flag.StringVar(&config.ConnectTLSCA, "connect-tls-ca", defaultConf.ConnectTLSCA, "CA certificate file for verifying upstream LNS")
	flag.BoolVar(&config.ConnectTLSInsecure, "connect-tls-insecure", defaultConf.ConnectTLSInsecure, "skip TLS verification for upstream LNS")
	flag.StringVar(&config.ConnectDataTLSServerName, "connect-data-tls-server-name", defaultConf.ConnectDataTLSServerName, "override TLS ServerName for data connection when server cert hostname differs (e.g. lns-tpw.thingpark.com)")
	flag.StringVar(&config.ConnectDataTLSCA, "connect-data-tls-ca", defaultConf.ConnectDataTLSCA, "CA file for data connection TLS when LNS uses a different CA than connect-uri")
	flag.BoolVar(&config.ConnectDataTLSInsecure, "connect-data-tls-insecure", defaultConf.ConnectDataTLSInsecure, "skip server certificate verification for data connection only (client cert still sent)")
	flag.StringVar(&config.CertsDir, "certs-dir", defaultConf.CertsDir, "directory for CA, bootstrap and per-gateway certs")
	flag.StringVar(&config.CertsCACert, "certs-ca-cert", defaultConf.CertsCACert, "path to existing CA cert (default: generate in certs-dir)")
	flag.StringVar(&config.CertsCAKey, "certs-ca-key", defaultConf.CertsCAKey, "path to existing CA key (use with --certs-ca-cert)")
	flag.BoolVar(&config.CertsUpstreamClientCert, "certs-upstream-client-cert", defaultConf.CertsUpstreamClientCert, "use proxy-issued client cert for CUPS when no server-issued cert; set true only if upstream LNS/CUPS trusts proxy CA (e.g. your own LNS)")

	// Analytics client config
	flag.StringVar(&config.ClientId, "client-id", defaultConf.ClientId, "the client ID to use for connecting to Kudzu Analytics")
	flag.StringVar(&config.ClientKey, "client-key", defaultConf.ClientKey, "the private client key to use for connecting to Kudzu Analytics")
	flag.StringVar(&config.Endpoint, "analytics-endpoint", defaultConf.Endpoint, "the analytics endpoint to push the data to")
	flag.StringVar(&config.PairingEndpoint, "pairing-endpoint", defaultConf.PairingEndpoint, "the HTTPS base URL used for pairing")
	flag.StringVar(&config.CAFile, "analytics-ca-file", defaultConf.CAFile, "optional CA certificate file appended to the system trust store for analytics TLS")
	flag.IntVar(&config.ConnectTimeout, "analytics-connect-timeout", defaultConf.ConnectTimeout, "how long to wait for analytics connection")
	flag.IntVar(&config.RequestTimeout, "analytics-request-timeout", defaultConf.RequestTimeout, "how long to wait for analytics to be pushed")
	flag.IntVar(&config.MaxReconnectBackoff, "analytics-max-backoff", defaultConf.MaxReconnectBackoff, "the maximum time to wait for reconnecting")

	// Forwarder component config
	flag.IntVar(&config.FlushInterval, "flush-interval", defaultConf.FlushInterval, "how frequently to flush collected metrics to analytics")
	flag.StringVar(&config.GatewayId, "gateway", defaultConf.GatewayId, "the ID of the gateway the forwarder is pushing data for")
	flag.BoolVar(&config.GaugeStat, "gauge-stat", defaultConf.GaugeStat, "the statistics are gauge values")
	flag.BoolVar(&config.ServerSide, "server-side", defaultConf.ServerSide, "the forwarder runs on the server-side")
	flag.IntVar(&config.MaxWSStreams, "max-ws-streams", defaultConf.MaxWSStreams, "max concurrent WebSocket gateway sessions (server-side)")

	flag.StringVar(&config.DebugDump, "debug-dump", defaultConf.DebugDump, "the filename where to write the traffic for debugging")
	flag.StringVar(&config.LogLevel, "log-level", defaultConf.LogLevel, "selects the verbosity of logging, can be 'error', 'warn', 'info', 'debug'")
	flag.BoolVar(&config.Verbose, "verbose", defaultConf.Verbose, "enable debug log level and hexdump of incoming/outgoing traffic (HTTP and WebSocket)")

	// Local flags
	var version bool
	var writeConfig bool
	var logFile string
	var pairPin string
	flag.BoolVar(&version, "version", false, "show the package version and exit")
	flag.BoolVar(&writeConfig, "write", false, "write any changes to the configuration file")
	flag.StringVar(&logFile, "log-file", "", "writes the program output to the specified logfile")
	flag.StringVar(&pairPin, "pair-pin", "", "if specified, tries to download a configuration from the server using this PIN and exits")

	flag.Parse()

	// Normalize URI fields: trim whitespace and surrounding double quotes (e.g. from config file or env)
	config.ConnectURI = strings.TrimSpace(config.ConnectURI)
	config.ConnectURI = strings.Trim(config.ConnectURI, `"`)
	config.ConnectDiscoveryURI = strings.TrimSpace(config.ConnectDiscoveryURI)
	config.ConnectDiscoveryURI = strings.Trim(config.ConnectDiscoveryURI, `"`)
	config.ConnectDataPath = strings.TrimSpace(config.ConnectDataPath)
	config.ConnectDataURI = strings.TrimSpace(config.ConnectDataURI)
	config.ConnectDataTLSServerName = strings.TrimSpace(config.ConnectDataTLSServerName)
	config.ConnectDataTLSCA = strings.TrimSpace(config.ConnectDataTLSCA)

	if version {
		fmt.Printf("Kudzu Analytics Basic Station Forwarder v%s (Git %s)\n", MajorVersion, Version())
		os.Exit(0)
	}

	if pairPin != "" {
		rendered, err := getRenderedPairConfig(pairPin, config)
		if err != nil {
			log.Fatalf("Could not pair with server: %s", err.Error())
		}
		if writeConfig {
			configPath := flag.DefaultConfigFlagname
			if f := flag.Lookup(flag.DefaultConfigFlagname); f != nil && f.Value.String() != "" {
				configPath = f.Value.String()
			}
			log.Infof("Writing changes to configuration file: %s", configPath)
			err = os.WriteFile(configPath, []byte(rendered), 0644)
			if err != nil {
				log.Fatalf("Could not write configuration file: %s", err.Error())
			}
		} else {
			fmt.Println(rendered)
		}
		os.Exit(0)
	}

	if config.ConnectURI == "" {
		log.Fatalf("You must specify an upstream LNS WebSocket URI (--connect-uri)")
	}
	if config.ClientId == "" {
		log.Fatalf("You must specify a client ID (--client-id=)")
	}
	if config.ClientKey == "" {
		log.Fatalf("You must specify a client Key (--client-key=)")
	}
	if config.GatewayId == "" && !config.ServerSide {
		log.Fatalf("You must specify a gateway ID (--gateway=) when running on the client-side")
	}

	if config.MaxWSStreams == 0 {
		if config.ServerSide {
			config.MaxWSStreams = 256
		} else {
			config.MaxWSStreams = 2
		}
	}

	if config.FlushInterval == 0 {
		if config.ServerSide {
			config.FlushInterval = 5
		} else {
			config.FlushInterval = 10
		}
	}

	if config.Verbose {
		config.LogLevel = "debug"
	}

	switch config.LogLevel {
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	case "warn":
		log.SetLevel(log.WarnLevel)
	default:
		log.Fatalf("Unknown log level: %s", config.LogLevel)
	}

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0755)
		if err != nil {
			log.Fatalf("Could not open logfile %s for writing: %s", logFile, err.Error())
		}
		log.SetOutput(f)
	}

	b, _ := json.Marshal(config)
	log.Debugf("Debug configuration: %s", string(b))

	return config
}
