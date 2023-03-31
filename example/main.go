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
		ClientId:  "63eb768c6e38edf35cf5bd1b",
		ClientKey: "a6f39cdf2f613fd85e8c7665076de1c63ab8b61b35fce490",
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
		up.Ant = []*pb.AnalyticsUplinkAntenna{{
			Antenna: 0,
			IfChan:  1,
			RSSIC:   10,
			LSNR:    11.32,
		}}
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

	// Identify the gateway you are uploading the details for
	analytics.GatewayId = "63ab72c094323ef9d802f32e"

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
