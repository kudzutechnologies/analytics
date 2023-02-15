package main

import (
	"github.com/namsral/flag"
	log "github.com/sirupsen/logrus"
)

type ForwarderConfig struct {
	QueueSize           int
	BufferSize          int
	LocalAddress        string
	RemoteAddress       string
	ConnectTimeout      int
	RequestTimeout      int
	MaxReconnectBackoff int
	FlushInterval       int
	ClientId            string
	ClientKey           string
	Endpoint            string
	GatewayId           string
	GaugeStat           bool
}

func ParseConfigFromEnv() ForwarderConfig {
	var config ForwarderConfig

	// UDP forwarder config
	flag.IntVar(&config.QueueSize, "queue-size", 100, "how many items to keep in the queue")
	flag.IntVar(&config.BufferSize, "buffer-size", 1024, "how much memory to allocate for the UDP packets")
	flag.StringVar(&config.LocalAddress, "local", "127.0.0.1:1700", "the local endpoint to listen for UDP packet forwarder")
	flag.StringVar(&config.RemoteAddress, "remote", "", "the remote endpoint where to forward the received data")

	// Analytics client config
	flag.StringVar(&config.ClientId, "client-id", "", "the client ID to use for connecting to Kudzu Analytics")
	flag.StringVar(&config.ClientKey, "client-key", "", "the private client key to use for connecting to Kudzu Analytics")
	flag.StringVar(&config.Endpoint, "analytics-endpoint", "", "the analytics endpoint to push the data to")
	flag.IntVar(&config.ConnectTimeout, "analytics-connect-timeout", 0, "how long to wait for analytics connection")
	flag.IntVar(&config.RequestTimeout, "analytics-request-timeout", 0, "how long to wait for analytics to be pushed")
	flag.IntVar(&config.MaxReconnectBackoff, "analytics-max-backoff", 0, "the maximum time to wait for reconnecting")

	// Forwarder component config
	flag.IntVar(&config.FlushInterval, "flush-interval", 30, "how frequently to flush collected metrics to analytics")
	flag.StringVar(&config.GatewayId, "gateway", "", "the ID of the gateway the forwarder is pushing data for")
	flag.BoolVar(&config.GaugeStat, "gauge-stat", false, "the statistics are gauge values")

	flag.Parse()

	if config.RemoteAddress == "" {
		log.Fatalf("You must specify a remote endpoint (--remote)")
	}
	if config.ClientId == "" {
		log.Fatalf("You must specify a client ID (--client-id=)")
	}
	if config.ClientKey == "" {
		log.Fatalf("You must specify a client Key (--client-key=)")
	}
	if config.GatewayId == "" {
		log.Fatalf("You must specify a gateway ID (--gateway=)")
	}

	return config
}
