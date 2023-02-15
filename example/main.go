package main

import (
	"log"
	"time"

	pb "github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func main() {

	// Open a client session,
	client := client.CreateAnalyticsClient(client.AnalyticsClientConfig{
		ClientId:  "17629e8de7e2e464",
		ClientKey: "5d72af3891b8c452cea09a76dde7739b",
	})

	// Connect to server
	log.Printf("Connecting to server")
	err := client.Connect()
	if err != nil {
		log.Fatalf("Could not connect: %s", err.Error())
	}

	// Compose a bulk of metrics to push
	log.Printf("Pushing metrics")
	var analytics pb.AnalyticsMetrics
	for i := 0; i < 10; i += 1 {
		// Compose an uplink frame
		var up pb.AnalyticsUplink
		up.Channel = 1
		up.Frequency = 868.000
		up.Fhdr = []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
		analytics.Uplinks = append(analytics.Uplinks, &up)

		// Compose a downlink frame
		var dwn pb.AnalyticsDownlink
		dwn.Channel = 2
		dwn.Frequency = 868.000
		dwn.Fhdr = []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
		analytics.Downlinks = append(analytics.Downlinks, &dwn)
	}

	// Push metrics
	for {
		err = client.PushMetrics(&analytics)
		if err != nil {
			if grpc.Code(err) == codes.DeadlineExceeded {
				log.Printf("Disconnected")
			}
			log.Fatalf("could not push metrics: %v", err)
		}

		log.Printf("Data pushed")
		time.Sleep(1 * time.Second)
	}

}
