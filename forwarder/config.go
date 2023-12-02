package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/namsral/flag"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

const MajorVersion = "0.1.8"

type ForwarderConfig struct {
	BufferSize           int
	ClientId             string
	ClientKey            string
	ConnectHost          string
	ConnectInterface     string
	ConnectPortDown      int
	ConnectPortUp        int
	ConnectRetryInterval int
	ConnectTimeout       int
	DebugDump            string
	Endpoint             string
	FlushInterval        int
	GatewayId            string
	GaugeStat            bool
	ListenHost           string
	ListenPortDown       int
	ListenPortUp         int
	LogLevel             string
	MaxReconnectBackoff  int
	MaxUDPStreams        int
	QueueSize            int
	RequestTimeout       int
	ServerSide           bool
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
	flag.IntVar(&config.QueueSize, "queue-size", 100, "how many items to keep in the queue")
	flag.IntVar(&config.BufferSize, "buffer-size", 1500, "how much memory to allocate for the UDP packets")
	flag.StringVar(&config.ListenHost, "listen-host", "127.0.0.1", "the hostname where to listen (UDP forwarder connects here)")
	flag.IntVar(&config.ListenPortUp, "listen-port-up", 1800, "the (local) port where to receive uplink datagrams from the UDP forwarder")
	flag.IntVar(&config.ListenPortDown, "listen-port-down", 1801, "the UDP forwarder port where to send downlink datagrams to")
	flag.StringVar(&config.ConnectHost, "connect-host", "", "the hostname where to connect to (the LoRa Server)")
	flag.IntVar(&config.ConnectPortUp, "connect-port-up", 1700, "the server port where to send uplink datagrams to")
	flag.IntVar(&config.ConnectPortDown, "connect-port-down", 1700, "the (local) port where to receive downlink datagrams from")
	flag.StringVar(&config.ConnectInterface, "connect-interface", "0.0.0.0", "the interface to bind when connecting to remote host")
	flag.IntVar(&config.MaxUDPStreams, "max-udp-streams", 0, "how many distinct UDP streams to maintain. Only useful on server-side mode")
	flag.IntVar(&config.ConnectRetryInterval, "connect-retry-interval", 1, "how many seconds to wait before re-connecting to the remote server if the connection is severed")

	// Analytics client config
	flag.StringVar(&config.ClientId, "client-id", "", "the client ID to use for connecting to Kudzu Analytics")
	flag.StringVar(&config.ClientKey, "client-key", "", "the private client key to use for connecting to Kudzu Analytics")
	flag.StringVar(&config.Endpoint, "analytics-endpoint", "", "the analytics endpoint to push the data to")
	flag.IntVar(&config.ConnectTimeout, "analytics-connect-timeout", 0, "how long to wait for analytics connection")
	flag.IntVar(&config.RequestTimeout, "analytics-request-timeout", 0, "how long to wait for analytics to be pushed")
	flag.IntVar(&config.MaxReconnectBackoff, "analytics-max-backoff", 0, "the maximum time to wait for reconnecting")

	// Forwarder component config
	flag.IntVar(&config.FlushInterval, "flush-interval", 0, "how frequently to flush collected metrics to analytics")
	flag.StringVar(&config.GatewayId, "gateway", "", "the ID of the gateway the forwarder is pushing data for")
	flag.BoolVar(&config.GaugeStat, "gauge-stat", false, "the statistics are gauge values")
	flag.BoolVar(&config.ServerSide, "server-side", false, "the forwarder runs on the server-side")

	flag.StringVar(&config.DebugDump, "debug-dump", "", "the filename where to write the traffic for debugging")
	flag.StringVar(&config.LogLevel, "log-level", "info", "selects the verbosity of logging, can be 'error', 'warn', 'info', 'debug'")

	// Local flags
	var version bool
	var logFile string
	flag.BoolVar(&version, "version", false, "show the package version and exit")
	flag.StringVar(&logFile, "log-file", "", "writes the program output to the specified logfile")

	flag.Parse()

	// Check if only version is requested
	if version {
		fmt.Printf("Kudzu Analytics UDP Packet Forwarder v%s (Git %s)\n", MajorVersion, Version())
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
		logrus.SetOutput(f)
	}

	// Dump the default config
	b, _ := json.Marshal(config)
	log.Debugf("Debug configuration: %s", string(b))

	return config
}
