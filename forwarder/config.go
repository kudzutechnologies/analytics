package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/namsral/flag"
	log "github.com/sirupsen/logrus"
)

const MajorVersion = "0.1.11"

type ForwarderConfig struct {
	BufferSize           int    `json:"buffer-size,omitempty"`
	ClientId             string `json:"client-id,omitempty"`
	ClientKey            string `json:"client-key,omitempty"`
	ConnectHost          string `json:"connect-host,omitempty"`
	ConnectInterface     string `json:"connect-interface,omitempty"`
	ConnectPortDown      int    `json:"connect-port-down,omitempty"`
	ConnectPortUp        int    `json:"connect-port-up,omitempty"`
	ConnectRetryInterval int    `json:"connect-retry-interval,omitempty"`
	ConnectTimeout       int    `json:"connect-timeout,omitempty"`
	DebugDump            string `json:"debug-dump,omitempty"`
	Endpoint             string `json:"analytics-endpoint,omitempty"`
	FlushInterval        int    `json:"flush-interval,omitempty"`
	GatewayId            string `json:"gateway,omitempty"`
	GaugeStat            bool   `json:"gauge-stat,omitempty"`
	ListenHost           string `json:"listen-host,omitempty"`
	ListenPortDown       int    `json:"listen-port-down,omitempty"`
	ListenPortUp         int    `json:"listen-port-up,omitempty"`
	LogLevel             string `json:"log-level,omitempty"`
	MaxReconnectBackoff  int    `json:"analytics-max-backoff,omitempty"`
	MaxUDPStreams        int    `json:"max-udp-streams,omitempty"`
	QueueSize            int    `json:"queue-size,omitempty"`
	RequestTimeout       int    `json:"analytics-request-timeout,omitempty"`
	ServerSide           bool   `json:"server-side,omitempty"`
}

var defaultConf = ForwarderConfig{
	BufferSize:           1500,
	ClientId:             "",
	ClientKey:            "",
	ConnectHost:          "",
	ConnectInterface:     "0.0.0.0",
	ConnectPortDown:      1700,
	ConnectPortUp:        1700,
	ConnectRetryInterval: 1,
	ConnectTimeout:       0,
	DebugDump:            "",
	Endpoint:             "",
	FlushInterval:        0,
	GatewayId:            "",
	GaugeStat:            false,
	ListenHost:           "127.0.0.1",
	ListenPortDown:       1801,
	ListenPortUp:         1800,
	LogLevel:             "info",
	MaxReconnectBackoff:  0,
	MaxUDPStreams:        0,
	QueueSize:            100,
	RequestTimeout:       0,
	ServerSide:           false,
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

	// UDP forwarder config
	flag.IntVar(&config.QueueSize, "queue-size", defaultConf.QueueSize, "how many items to keep in the queue")
	flag.IntVar(&config.BufferSize, "buffer-size", defaultConf.BufferSize, "how much memory to allocate for the UDP packets")
	flag.StringVar(&config.ListenHost, "listen-host", defaultConf.ListenHost, "the hostname where to listen (UDP forwarder connects here)")
	flag.IntVar(&config.ListenPortUp, "listen-port-up", defaultConf.ListenPortUp, "the (local) port where to receive uplink datagrams from the UDP forwarder")
	flag.IntVar(&config.ListenPortDown, "listen-port-down", defaultConf.ListenPortDown, "the UDP forwarder port where to send downlink datagrams to")
	flag.StringVar(&config.ConnectHost, "connect-host", defaultConf.ConnectHost, "the hostname where to connect to (the LoRa Server)")
	flag.IntVar(&config.ConnectPortUp, "connect-port-up", defaultConf.ConnectPortUp, "the server port where to send uplink datagrams to")
	flag.IntVar(&config.ConnectPortDown, "connect-port-down", defaultConf.ConnectPortDown, "the (local) port where to receive downlink datagrams from")
	flag.StringVar(&config.ConnectInterface, "connect-interface", defaultConf.ConnectInterface, "the interface to bind when connecting to remote host")
	flag.IntVar(&config.MaxUDPStreams, "max-udp-streams", defaultConf.MaxUDPStreams, "how many distinct UDP streams to maintain. Only useful on server-side mode")
	flag.IntVar(&config.ConnectRetryInterval, "connect-retry-interval", defaultConf.ConnectRetryInterval, "how many seconds to wait before re-connecting to the remote server if the connection is severed")

	// Analytics client config
	flag.StringVar(&config.ClientId, "client-id", defaultConf.ClientId, "the client ID to use for connecting to Kudzu Analytics")
	flag.StringVar(&config.ClientKey, "client-key", defaultConf.ClientKey, "the private client key to use for connecting to Kudzu Analytics")
	flag.StringVar(&config.Endpoint, "analytics-endpoint", defaultConf.Endpoint, "the analytics endpoint to push the data to")
	flag.IntVar(&config.ConnectTimeout, "analytics-connect-timeout", defaultConf.ConnectTimeout, "how long to wait for analytics connection")
	flag.IntVar(&config.RequestTimeout, "analytics-request-timeout", defaultConf.RequestTimeout, "how long to wait for analytics to be pushed")
	flag.IntVar(&config.MaxReconnectBackoff, "analytics-max-backoff", defaultConf.MaxReconnectBackoff, "the maximum time to wait for reconnecting")

	// Forwarder component config
	flag.IntVar(&config.FlushInterval, "flush-interval", defaultConf.FlushInterval, "how frequently to flush collected metrics to analytics")
	flag.StringVar(&config.GatewayId, "gateway", defaultConf.GatewayId, "the ID of the gateway the forwarder is pushing data for")
	flag.BoolVar(&config.GaugeStat, "gauge-stat", defaultConf.GaugeStat, "the statistics are gauge values")
	flag.BoolVar(&config.ServerSide, "server-side", defaultConf.ServerSide, "the forwarder runs on the server-side")

	flag.StringVar(&config.DebugDump, "debug-dump", defaultConf.DebugDump, "the filename where to write the traffic for debugging")
	flag.StringVar(&config.LogLevel, "log-level", defaultConf.LogLevel, "selects the verbosity of logging, can be 'error', 'warn', 'info', 'debug'")

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

	// Check if only version is requested
	if version {
		fmt.Printf("Kudzu Analytics UDP Packet Forwarder v%s (Git %s)\n", MajorVersion, Version())
		os.Exit(0)
	}

	// If we only need to pair, download pair config and write config file now
	if pairPin != "" {
		config, err := getRenderedPairConfig(pairPin, config)
		if err != nil {
			log.Fatalf("Could not pair with server: %s", err.Error())
		}

		if writeConfig {
			// Write the configuration to the file
			log.Infof("Writing changes to configuration file: %s", flag.DefaultConfigFlagname)
			err = os.WriteFile(flag.DefaultConfigFlagname, []byte(config), 0644)
			if err != nil {
				log.Fatalf("Could not write configuration file: %s", err.Error())
			}
		} else {
			fmt.Println(config)
		}
		os.Exit(0)
	}

	if config.ConnectHost == "" {
		log.Fatalf("You must specify a LoRa server to connect to (--connect-host)")
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

	// Adjust MaxUDPStreams defaults
	if config.MaxUDPStreams == 0 {
		if config.ServerSide {
			// On server-side environments, accept streams from many gaateways
			config.MaxUDPStreams = 256
		} else {
			// In low-resource environments we shouldn't create too many streams
			config.MaxUDPStreams = 2
		}
	}

	// Adjust flush interval defaults
	if config.FlushInterval == 0 {
		if config.ServerSide {
			config.FlushInterval = 5
		} else {
			config.FlushInterval = 10
		}
	}

	// Apply log level
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

	// If we have a logfile specified, redirect output now
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0755)
		if err != nil {
			log.Fatalf("Could not open logfile %s for writing: %s", logFile, err.Error())
		}
		log.SetOutput(f)
	}

	// Dump the default config
	b, _ := json.Marshal(config)
	log.Debugf("Debug configuration: %s", string(b))

	return config
}
