package main

import (
	"context"
	"log"
	"time"

	pb "github.com/kudzutechnologies/analytics/api"
	"google.golang.org/grpc"
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

	// Contact the server and print out its response.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	//
	var analytics pb.AnalyticsMetrics
	var up pb.AnalyticsUplink
	analytics.Uplinks = append(analytics.Uplinks, &up)

	r, err := c.PushMetrics(ctx, &analytics)
	if err != nil {
		log.Fatalf("could not push metrics: %v", err)
	}
	log.Printf("Server responded with: %s", r.GetStatusMessage())
}
