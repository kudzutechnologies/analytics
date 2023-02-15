package main

import (
	"github.com/kudzutechnologies/analytics/client"
	log "github.com/sirupsen/logrus"
)

func main() {
	// Parse configuration from environment
	config := ParseConfigFromEnv()

	// Connect to the analytics endpoint
	client := client.CreateAnalyticsClient(client.AnalyticsClientConfig{
		ClientId:            config.ClientId,
		ClientKey:           config.ClientKey,
		Endpoint:            config.Endpoint,
		ConnectTimeout:      int32(config.ConnectTimeout),
		RequestTimeout:      int32(config.RequestTimeout),
		MaxReconnectBackoff: int32(config.MaxReconnectBackoff),
	})

	// Create the UDP proxy
	proxy, err := CreateUDPProxy(config)
	if err != nil {
		log.Fatalf("Could not start forwarder: %s", err.Error())
	}

	// Try to connect to the analytics endpoint
	fw := CreateAnalyticsForwarder(config, client, proxy)
	fw.StartAndWait()
}
