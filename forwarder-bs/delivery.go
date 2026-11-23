package forwarderbs

import (
	"context"
	"sync"
	"time"

	"github.com/kudzutechnologies/analytics/api"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type queuedMetrics struct {
	key              string
	data             *api.AnalyticsMetrics
	bytes, messages  int
	sealed, inFlight bool
}

type DeliveryStats struct {
	Batches, Messages, Bytes                                                 int
	DroppedMessages, PushFailures                                            uint64
	CapacityDrops, OversizeDrops, ClosedDrops, InvalidDrops, ConnectFailures uint64
}

// The queue owns every admitted object through partial, retry and in-flight
// states. Wire-size charges are conservative when multiple records are merged.
type deliveryQueue struct {
	mu                                                                       sync.Mutex
	items                                                                    []*queuedMetrics
	maxBatches, maxMessages, maxBytes, bytes                                 int
	dropped, failures                                                        uint64
	capacityDrops, oversizeDrops, closedDrops, invalidDrops, connectFailures uint64
	closed                                                                   bool
	wake                                                                     chan struct{}
}

func newDeliveryQueue(c ForwarderConfig) *deliveryQueue {
	return &deliveryQueue{maxBatches: c.MaxBatches, maxMessages: c.MaxMessages, maxBytes: c.MaxBufferBytes, wake: make(chan struct{}, 1)}
}
func messageCount(m *api.AnalyticsMetrics) int {
	return len(m.Uplinks) + len(m.Downlinks) + len(m.Stats)
}
func (q *deliveryQueue) notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
func (q *deliveryQueue) add(key string, m *api.AnalyticsMetrics, evict bool) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	n, b := messageCount(m), proto.Size(m)
	if n == 0 {
		return true
	}
	if q.closed || n > q.maxMessages || b > q.maxBytes {
		q.dropped += uint64(n)
		if q.closed {
			q.closedDrops += uint64(n)
		} else {
			q.oversizeDrops += uint64(n)
		}
		return false
	}
	for {
		var tail *queuedMetrics
		if len(q.items) > 0 {
			last := q.items[len(q.items)-1]
			if !last.sealed && !last.inFlight && last.key == key && last.messages+n <= q.maxMessages {
				tail = last
			}
		}
		if q.bytes+b <= q.maxBytes && (tail != nil || len(q.items) < q.maxBatches) {
			if tail == nil {
				if len(q.items) > 0 {
					q.items[len(q.items)-1].sealed = true
				}
				tail = &queuedMetrics{key: key, data: m}
				q.items = append(q.items, tail)
			} else {
				proto.Merge(tail.data, m)
			}
			tail.bytes += b
			tail.messages += n
			q.bytes += b
			if tail.messages >= q.maxMessages {
				tail.sealed = true
			}
			q.notify()
			return true
		}
		index := -1
		if evict {
			for i, item := range q.items {
				if !item.inFlight {
					index = i
					break
				}
			}
		}
		if index < 0 {
			q.dropped += uint64(n)
			q.capacityDrops += uint64(n)
			return false
		}
		q.remove(index, true)
	}
}
func (q *deliveryQueue) remove(i int, drop bool) {
	v := q.items[i]
	q.bytes -= v.bytes
	if drop {
		q.dropped += uint64(v.messages)
		q.capacityDrops += uint64(v.messages)
	}
	copy(q.items[i:], q.items[i+1:])
	q.items[len(q.items)-1] = nil
	q.items = q.items[:len(q.items)-1]
}
func (q *deliveryQueue) seal(closeQueue bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, v := range q.items {
		v.sealed = true
	}
	q.closed = q.closed || closeQueue
	q.notify()
}
func (q *deliveryQueue) next() (*queuedMetrics, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil, q.closed
	}
	v := q.items[0]
	if !v.sealed || v.inFlight {
		return nil, false
	}
	v.inFlight = true
	return v, false
}
func (q *deliveryQueue) complete(v *queuedMetrics, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err != nil {
		q.failures++
	}
	for i, item := range q.items {
		if item == v {
			if err == nil || status.Code(err) == codes.InvalidArgument {
				if err != nil {
					q.invalidDrops += uint64(item.messages)
					q.dropped += uint64(item.messages)
				}
				q.remove(i, false)
			} else {
				item.inFlight = false
			}
			break
		}
	}
	q.notify()
}
func (q *deliveryQueue) snapshot() DeliveryStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := DeliveryStats{Batches: len(q.items), Bytes: q.bytes, DroppedMessages: q.dropped, PushFailures: q.failures, CapacityDrops: q.capacityDrops, OversizeDrops: q.oversizeDrops, ClosedDrops: q.closedDrops, InvalidDrops: q.invalidDrops, ConnectFailures: q.connectFailures}
	for _, v := range q.items {
		s.Messages += v.messages
	}
	return s
}

type analyticsTransport interface {
	ConnectContext(context.Context) error
	PushMetricsOnce(context.Context, *api.AnalyticsMetrics) error
	Disconnect() error
}

func (f *AnalyticsForwarder) Start() {
	f.startOnce.Do(func() { ctx, cancel := context.WithCancel(context.Background()); f.cancel = cancel; go f.sendLoop(ctx) })
}
func (f *AnalyticsForwarder) Stop(ctx context.Context) bool {
	f.Start()
	f.collectMu.Lock()
	f.stopped = true
	f.collectMu.Unlock()
	f.flushData()
	f.queue.seal(true)
	select {
	case <-f.done:
	case <-ctx.Done():
		f.cancel()
		<-f.done
	}
	f.cancel()
	return f.queue.snapshot().Batches == 0
}
func (f *AnalyticsForwarder) DeliveryStats() DeliveryStats { return f.queue.snapshot() }
func (f *AnalyticsForwarder) sendLoop(ctx context.Context) {
	defer close(f.done)
	if f.backend == nil {
		<-ctx.Done()
		return
	}
	defer f.backend.Disconnect()
	ticker := time.NewTicker(time.Duration(f.config.FlushInterval) * time.Second)
	defer ticker.Stop()
	connected := false
	backoff := time.Second
	wait := func() bool {
		timer := time.NewTimer(backoff)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
		}
		max := time.Duration(f.config.MaxReconnectBackoff) * time.Second
		backoff *= 2
		if backoff > max {
			backoff = max
		}
		return true
	}
	for {
		if ctx.Err() != nil {
			return
		}
		f.queue.mu.Lock()
		finishedEmpty := f.queue.closed && len(f.queue.items) == 0
		f.queue.mu.Unlock()
		if finishedEmpty {
			return
		}
		if !connected {
			attempt, cancel := context.WithTimeout(ctx, time.Duration(f.config.ConnectTimeout)*time.Second)
			err := f.backend.ConnectContext(attempt)
			cancel()
			if err != nil {
				f.queue.mu.Lock()
				f.queue.connectFailures++
				f.queue.mu.Unlock()
				if ctx.Err() == nil {
					instanceLogger(f.config.Logger).Warnf("analytics connection failed retry_seconds=%d", int(backoff/time.Second))
				}
				if !wait() {
					return
				}
				continue
			}
			connected = true
		}
		item, finished := f.queue.next()
		if finished {
			return
		}
		if item == nil {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				f.flushData()
			case <-f.queue.wake:
			}
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, time.Duration(f.config.RequestTimeout)*time.Second)
		err := f.backend.PushMetricsOnce(attempt, item.data)
		cancel()
		f.queue.complete(item, err)
		if err != nil {
			if ctx.Err() == nil {
				instanceLogger(f.config.Logger).Warnf("analytics push failed status=%s retry_seconds=%d", status.Code(err), int(backoff/time.Second))
			}
			connected = false
			_ = f.backend.Disconnect()
			if !wait() {
				return
			}
		} else {
			backoff = time.Second
		}
	}
}
