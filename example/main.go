package main

import (
	"context"
	"log"
	"time"

	pb "github.com/kudzutechnologies/analytics/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

const (
	address     = "localhost:50051"
	defaultName = "world"
)

func main() {
	// Set up a connection to the server.
	conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewAnalyticsServerClient(conn)

	// Contact the API gateway
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	log.Printf("Connected")

	// Open a client session to the gateway,
	netId := "1abc012293129a"
	sess, _ := pb.CreateBasicServiceClientSession(netId)

	// Compose a bulk of metrics to push
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
	req, _ := sess.CreatePushMetricsRequest(&analytics)
	r, err := c.PushMetrics(ctx, req)
	if err != nil {
		if grpc.Code(err) == codes.DeadlineExceeded {
			log.Printf("Disconnected")
		}
		log.Fatalf("could not push metrics: %v", err)
	}

	log.Printf("Server responded with: %s", r.GetStatusMessage())
}
