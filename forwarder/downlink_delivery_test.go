package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"net"
	"sync"
	"testing"

	"github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
	"github.com/stretchr/testify/require"
)

func deliveryPacket(kind byte, token uint16, eui []byte, body string) []byte {
	packet := []byte{2, 0, 0, kind}
	binary.LittleEndian.PutUint16(packet[1:], token)
	return append(append(packet, eui...), []byte(body)...)
}

const deliveryCommand = `{"txpk":{"tmst":42,"freq":868.3,"rfch":1,"powe":14,"modu":"LORA","datr":"SF7BW125","codr":"4/5","size":13,"data":"YAQDAgEAAQABAAAAAA=="}}`

func TestDownlinkDeliveryObservations(t *testing.T) {
	peer := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1700}
	eui := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	f := CreateAnalyticsForwarder(ForwarderConfig{MaxUDPStreams: 10}, nil, nil)
	poll := deliveryPacket(PULL_DATA, 1, eui, "")
	f.DnLocalData(poll, peer)
	poll[4] = 99 // The UDP read buffer may be reused immediately after the callback.
	command := deliveryPacket(PULL_RESP, 0x1234, nil, deliveryCommand)
	f.DnRemoteData(command, peer)
	f.DnRemoteData(command, peer)
	frame := f.getMetricsFrame(peer)
	require.Len(t, frame.Downlinks, 2)
	metadata := frame.Downlinks[0].Delivery
	require.Equal(t, "eui:0102030405060708:1234", metadata.Id)
	hash := sha256.Sum256([]byte(deliveryCommand))
	require.Equal(t, hex.EncodeToString(hash[:]), metadata.CommandKey)
	require.Equal(t, metadata.CommandKey, frame.Downlinks[1].Delivery.CommandKey)
	require.NotZero(t, metadata.ObservedUnixMs)
	require.EqualValues(t, 42, frame.Downlinks[0].TxTime)
	require.Len(t, frame.Downlinks[0].PhyPayload, 13)

	for _, tc := range []struct {
		name, body, reason string
		status             api.DownlinkOutcomeStatus
	}{
		{"empty_success", "", "", api.DownlinkOutcomeStatus_OUTCOME_ACCEPTED},
		{"explicit_success", `{"txpk_ack":{"error":"NONE"}}`, "", api.DownlinkOutcomeStatus_OUTCOME_ACCEPTED},
		{"rejected", `{"txpk_ack":{"error":"TOO_LATE"}}`, "TOO_LATE", api.DownlinkOutcomeStatus_OUTCOME_REJECTED},
		{"unknown_code", `{"txpk_ack":{"error":"NEW_REJECTION"}}`, "NEW_REJECTION", api.DownlinkOutcomeStatus_OUTCOME_REJECTED},
		{"missing_error", `{"txpk_ack":{}}`, "ack_invalid", api.DownlinkOutcomeStatus_OUTCOME_UNKNOWN},
		{"null_error", `{"txpk_ack":{"error":null}}`, "ack_invalid", api.DownlinkOutcomeStatus_OUTCOME_UNKNOWN},
		{"wrong_type", `{"txpk_ack":{"error":1}}`, "ack_invalid", api.DownlinkOutcomeStatus_OUTCOME_UNKNOWN},
		{"malformed", `{`, "ack_invalid", api.DownlinkOutcomeStatus_OUTCOME_UNKNOWN},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.DnLocalData(deliveryPacket(TX_ACK, 0x1234, eui, tc.body), peer)
			outcome := frame.DownlinkOutcomes[len(frame.DownlinkOutcomes)-1]
			require.Equal(t, metadata.Id, outcome.Id)
			require.Equal(t, tc.status, outcome.Status)
			require.Equal(t, tc.reason, outcome.Reason)
			require.NotZero(t, outcome.ObservedUnixMs)
		})
	}
	f.metricsMu.Lock()
	batch := takeMetricsFrame(frame)
	f.metricsMu.Unlock()
	require.Len(t, batch.DownlinkOutcomes, 8)
	require.Empty(t, frame.DownlinkOutcomes)
	require.Empty(t, frame.Downlinks)
	require.Equal(t, eui, frame.GatewayEui, "gateway identity survives a flush")
	require.Zero(t, f.queueSize())
	f.DnLocalData(deliveryPacket(TX_ACK, 99, eui, ""), peer)
	require.Equal(t, 1, f.queueSize(), "ACK-only client-side batches must be sent")
	require.Empty(t, frame.Downlinks, "an ACK is not an extra packet")
	f.UpLocalData([]byte{2, 0, 0, TX_ACK}, peer)
	require.Len(t, frame.DownlinkOutcomes, 1, "truncated EUI is not a successful empty ACK")
}

func TestDownlinkDeliveryPeerIsolation(t *testing.T) {
	f := CreateAnalyticsForwarder(ForwarderConfig{MaxUDPStreams: 10}, nil, nil)
	var ids []string
	for _, port := range []int{1700, 1701} {
		peer := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
		eui := []byte{0, 0, 0, 0, 0, 0, 0, byte(port)}
		f.DnLocalData(deliveryPacket(PULL_DATA, 1, eui, ""), peer)
		f.DnRemoteData(deliveryPacket(PULL_RESP, 1, nil, deliveryCommand), peer)
		ids = append(ids, f.getMetricsFrame(peer).Downlinks[0].Delivery.Id)
	}
	require.Equal(t, 2, f.metricsFrame.Len())
	require.NotEqual(t, ids[0], ids[1], "gateways behind one NAT must not share ACK identity")
}

func TestDownlinkDeliveryConcurrentBatchSnapshot(t *testing.T) {
	f := CreateAnalyticsForwarder(ForwarderConfig{MaxUDPStreams: 10}, nil, nil)
	peer := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1700}
	packet := deliveryPacket(TX_ACK, 1, []byte{1, 2, 3, 4, 5, 6, 7, 8}, "")
	var workers sync.WaitGroup
	for range []int{0, 1, 2, 3} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := 0; i < 100; i++ {
				f.DnLocalData(packet, peer)
			}
		}()
	}
	var total int
	for i := 0; i < 100; i++ {
		f.metricsMu.Lock()
		for _, frame := range f.metricsFrame.Values() {
			total += len(takeMetricsFrame(frame).DownlinkOutcomes)
		}
		f.metricsMu.Unlock()
	}
	workers.Wait()
	f.metricsMu.Lock()
	for _, frame := range f.metricsFrame.Values() {
		total += len(takeMetricsFrame(frame).DownlinkOutcomes)
	}
	f.metricsMu.Unlock()
	require.Equal(t, 400, total, "concurrent flushes must neither lose nor duplicate observations")
}

func TestDownlinkConfiguredIdentityBeforeGatewayPoll(t *testing.T) {
	peer := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1700}
	before := deliveryID(nil, "configured", peer, 1)
	after := deliveryID([]byte{1, 2, 3, 4, 5, 6, 7, 8}, "configured", peer, 1)
	require.Equal(t, before, after, "a first ACK must retain configured identity even when EUI becomes known")
}

func TestDownlinkDeliveryGatewayChangeFlushesOldEnvelope(t *testing.T) {
	peer := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1700}
	c := client.CreateAnalyticsClient(client.AnalyticsClientConfig{})
	f := CreateAnalyticsForwarder(ForwarderConfig{MaxUDPStreams: 10}, c, nil)
	first := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	second := []byte{8, 7, 6, 5, 4, 3, 2, 1}
	f.DnLocalData(deliveryPacket(PULL_DATA, 1, first, ""), peer)
	f.DnRemoteData(deliveryPacket(PULL_RESP, 1, nil, deliveryCommand), peer)
	previous := f.getMetricsFrame(peer)
	f.DnLocalData(deliveryPacket(PULL_DATA, 1, second, ""), peer)
	current := f.getMetricsFrame(peer)
	require.NotSame(t, previous, current)
	require.Equal(t, first, previous.GatewayEui, "flushed commands retain the original gateway envelope")
	require.Empty(t, previous.Downlinks, "old batch was handed to the transport, whose disconnected error is logged")
	require.Equal(t, second, current.GatewayEui)
	require.Empty(t, current.Downlinks, "new gateway cannot inherit old commands")
}

func TestDownlinkDeliveryFSK(t *testing.T) {
	peer := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1700}
	f := CreateAnalyticsForwarder(ForwarderConfig{MaxUDPStreams: 10, GatewayId: "configured"}, nil, nil)
	body := `{"txpk":{"modu":"FSK","datr":50000,"fdev":25000,"freq":868.3,"size":13,"data":"YAQDAgEAAQABAAAAAA=="}}`
	f.DnRemoteData(deliveryPacket(PULL_RESP, 1, nil, body), peer)
	frame := f.getMetricsFrame(peer)
	require.Len(t, frame.Downlinks, 1)
	require.EqualValues(t, 50000, frame.Downlinks[0].GetDataRateFSK())
	require.EqualValues(t, 25000, frame.Downlinks[0].FskFreqDev)
	require.NotNil(t, frame.Downlinks[0].Delivery)
}

func TestDownlinkDeliveryOutcomeBatchLimit(t *testing.T) {
	peer := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1700}
	c := client.CreateAnalyticsClient(client.AnalyticsClientConfig{})
	f := CreateAnalyticsForwarder(ForwarderConfig{MaxUDPStreams: 10}, c, nil)
	packet := deliveryPacket(TX_ACK, 1, []byte{1, 2, 3, 4, 5, 6, 7, 8}, "")
	for i := 0; i < 4097; i++ {
		f.DnLocalData(packet, peer)
	}
	frame := f.getMetricsFrame(peer)
	require.Len(t, frame.DownlinkOutcomes, 1, "full outcome batch was flushed before exceeding receiver capacity")
	require.Equal(t, "eui:0102030405060708:0001", frame.DownlinkOutcomes[0].Id)
}
