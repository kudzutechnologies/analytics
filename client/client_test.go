package client_test

import (
	"github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
)

func Example() {
	// Create a client
	c := client.CreateAnalyticsClient(client.AnalyticsClientConfig{
		ClientId:  "1122334455667788",
		ClientKey: "11223344556677889900aabbccddeeff",
	})

	// Connect to the server
	err := c.Connect()
	if err != nil {
		panic(err)
	}

	// Push analytics data
	metrics := &api.AnalyticsMetrics{}
	err = c.PushMetrics(metrics)
	if err != nil {
		panic(err)
	}

	// Disconnect the client
	c.Disconnect()
}
