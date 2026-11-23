package forwarderbs_test

import (
	"context"
	bs "github.com/kudzutechnologies/analytics/forwarder-bs"
	"net/http"
)

func Example() {
	c := bs.DefaultForwarderConfig()
	c.ConnectURI = "wss://lns.example"
	c.ClientId = "example-id"
	c.ClientKey = "example-key"
	c.GatewayId = "example-gateway"
	f, err := bs.New(c, bs.Dependencies{})
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err = f.Start(ctx); err != nil {
		panic(err)
	}
	server := &http.Server{Addr: "127.0.0.1:8090", Handler: f.Handler(), TLSConfig: f.TLSConfig()}
	_ = server // The host starts and shuts down its server.
	_ = f.Shutdown(context.Background())
}
