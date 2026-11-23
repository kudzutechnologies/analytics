package forwarderbs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
)

func TestSingleAttemptPushCanBeCancelled(t *testing.T) {
	entered := make(chan struct{})
	ep, ca := startAnalyticsFixture(t, &analyticsFixture{push: func(ctx context.Context, _ *api.AnalyticsMetrics) (*api.RespPush, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	c := client.CreateAnalyticsClient(client.AnalyticsClientConfig{Endpoint: ep, CAFile: ca, ClientId: "0011", ClientKey: "2233", ConnectTimeout: 1})
	if err := c.ConnectContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Disconnect()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.PushMetricsOnce(ctx, &api.AnalyticsMetrics{GatewayId: "fixture"}) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("push did not reach backend")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled push succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("push did not cancel")
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err := c.ConnectContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled connect: %v", err)
	}
}
