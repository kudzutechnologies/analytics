package forwarderbs

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDebugDumpBudget(t *testing.T) {
	var out bytes.Buffer
	dump := NewDebugDumpWriter(&out, 16)
	for i := 0; i < 10; i++ {
		dump.Write("up", "fixture", []byte("payload"), false)
	}
	if out.Len() != 16 {
		t.Fatalf("dump exceeded lifetime budget: %d", out.Len())
	}
}

func TestLossReasons(t *testing.T) {
	c := defaultConf
	c.MaxBatches = 1
	c.MaxMessages = 1
	c.MaxBufferBytes = 1024
	q := newDeliveryQueue(c)
	q.add("a", testRecord(), true)
	v, _ := q.next()
	q.add("b", testRecord(), true)
	q.complete(v, status.Error(codes.InvalidArgument, "fixture"))
	q.seal(true)
	q.add("a", testRecord(), true)
	s := q.snapshot()
	if s.DroppedMessages != 3 || s.CapacityDrops != 1 || s.InvalidDrops != 1 || s.ClosedDrops != 1 {
		t.Fatalf("loss accounting: %+v", s)
	}
}

func testRecord() *api.AnalyticsMetrics {
	return &api.AnalyticsMetrics{GatewayId: "fixture", Uplinks: []*api.AnalyticsUplink{{Fhdr: []byte{0xe0, 1, 2, 3}, Size: 4}}}
}

func TestQueueChargesInFlightAndRetainsFailure(t *testing.T) {
	c := defaultConf
	c.MaxBatches = 1
	c.MaxMessages = 1
	c.MaxBufferBytes = 1024
	q := newDeliveryQueue(c)
	if !q.add("a", testRecord(), true) {
		t.Fatal("first record rejected")
	}
	v, _ := q.next()
	if v == nil {
		t.Fatal("full batch not ready")
	}
	if q.add("a", testRecord(), true) {
		t.Fatal("in-flight batch did not consume capacity")
	}
	if s := q.snapshot(); s.Batches != 1 || s.Messages != 1 || s.Bytes > 1024 || s.DroppedMessages != 1 {
		t.Fatalf("limits: %+v", s)
	}
	q.complete(v, status.Error(codes.Unavailable, "outage"))
	v2, _ := q.next()
	if v2 != v {
		t.Fatal("failed payload was discarded")
	}
	q.complete(v2, nil)
	if q.snapshot().Batches != 0 {
		t.Fatal("acknowledged batch retained")
	}
}

func TestQueueOutageMemoryPlateaus(t *testing.T) {
	c := defaultConf
	c.MaxBatches = 2
	c.MaxMessages = 1
	c.MaxBufferBytes = 1024
	q := newDeliveryQueue(c)
	for i := 0; i < 10000; i++ {
		q.add("a", testRecord(), true)
		s := q.snapshot()
		if s.Batches > 2 || s.Messages > 2 || s.Bytes > 1024 {
			t.Fatalf("unbounded retention: %+v", s)
		}
	}
	if q.snapshot().DroppedMessages != 9998 {
		t.Fatal("overflow losses not counted")
	}
}

func TestConcurrentCollectionAndFlush(t *testing.T) {
	f := CreateAnalyticsForwarder(defaultConf, nil, nil)
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				switch n {
				case 0:
					f.OnUpstreamMessage("0102030405060708", []byte(`{"msgtype":"propdf","FRMPayload":"e00102","Freq":868100000}`), false)
				case 1:
					f.OnDownstreamMessage("0102030405060708", []byte(`{"msgtype":"dnmsg","pdu":"600102030400000001aabbccdd","RX1DR":0,"RX1Freq":868100000}`), false)
				case 2:
					f.flushData()
				case 3:
					f.DeliveryStats()
				}
			}
		}(n)
	}
	wg.Wait()
	s := f.DeliveryStats()
	if s.Batches > 2 || s.Bytes > 65536 {
		t.Fatalf("limits: %+v", s)
	}
}

func TestSenderShutdownCancelsBlockedPush(t *testing.T) {
	entered := make(chan struct{})
	ep, ca := startAnalyticsFixture(t, &analyticsFixture{push: func(ctx context.Context, _ *api.AnalyticsMetrics) (*api.RespPush, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	c := defaultConf
	c.Endpoint = ep
	c.CAFile = ca
	c.ClientId = "0011"
	c.ClientKey = "2233"
	c.MaxMessages = 1
	transport := client.CreateAnalyticsClient(client.AnalyticsClientConfig{Endpoint: c.Endpoint, CAFile: c.CAFile, ClientId: c.ClientId, ClientKey: c.ClientKey, ConnectTimeout: 1})
	f := CreateAnalyticsForwarder(c, transport, nil)
	f.Start()
	f.queue.add("a", testRecord(), true)
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("sender did not push")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if f.Stop(ctx) {
		t.Fatal("reported successful drain of cancelled push")
	}
	if time.Since(start) > 350*time.Millisecond {
		t.Fatal("shutdown exceeded deadline allowance")
	}
}
