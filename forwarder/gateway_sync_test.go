package main

import (
	"encoding/hex"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kudzutechnologies/analytics/api"
	"github.com/stretchr/testify/require"
)

func TestParseGatewaySyncConfigNone(t *testing.T) {
	parsed, err := ParseGatewaySyncConfig(ForwarderConfig{})
	require.NoError(t, err)
	require.Nil(t, parsed)
}

func TestParseGatewaySyncConfigRequiresIdentity(t *testing.T) {
	_, err := ParseGatewaySyncConfig(ForwarderConfig{
		GatewayName: "gw",
	})
	require.Error(t, err)
}

func TestParseGatewayLocation(t *testing.T) {
	loc, err := parseGatewayLocation("37.98,23.72")
	require.NoError(t, err)
	require.InDelta(t, 37.98, loc.Latitude, 1e-9)
	require.InDelta(t, 23.72, loc.Longitude, 1e-9)
	require.Nil(t, loc.Altitude)

	loc, err = parseGatewayLocation("37.98,23.72,12.5")
	require.NoError(t, err)
	require.NotNil(t, loc.Altitude)
	require.InDelta(t, 12.5, *loc.Altitude, 1e-9)

	_, err = parseGatewayLocation("91,0")
	require.Error(t, err)
	_, err = parseGatewayLocation("0,181")
	require.Error(t, err)
	_, err = parseGatewayLocation("1")
	require.Error(t, err)
}

func TestParseGatewayEUI(t *testing.T) {
	b, err := parseGatewayEUI("01:02:03:04:05:06:07:08")
	require.NoError(t, err)
	require.Equal(t, "0102030405060708", hex.EncodeToString(b))

	_, err = parseGatewayEUI("01020304")
	require.Error(t, err)
	_, err = parseGatewayEUI("zzzzzzzzzzzzzzzz")
	require.Error(t, err)
}

func TestParseGatewayRadios(t *testing.T) {
	radios, err := parseGatewayRadios("0:27,1:14:-130")
	require.NoError(t, err)
	require.Len(t, radios, 2)
	require.Equal(t, int32(0), radios[0].RfChain)
	require.InDelta(t, float32(27), radios[0].MaxTxPower, 1e-3)
	require.Nil(t, radios[0].GainSensitivity)
	require.Equal(t, int32(1), radios[1].RfChain)
	require.NotNil(t, radios[1].GainSensitivity)
	require.InDelta(t, float32(-130), *radios[1].GainSensitivity, 1e-3)

	_, err = parseGatewayRadios("0:27,0:14")
	require.Error(t, err)
	_, err = parseGatewayRadios("bad")
	require.Error(t, err)
}

func TestParseGatewaySyncConfigFull(t *testing.T) {
	parsed, err := ParseGatewaySyncConfig(ForwarderConfig{
		GatewayId:       "deadbeefdeadbeefdeadbeef",
		GatewayEID:      "edge-1",
		GatewayEUI:      "0102030405060708",
		GatewayName:     "Roof GW",
		GatewayLocation: "37.9,23.7,10",
		GatewayRadios:   "0:27",
	})
	require.NoError(t, err)
	require.NotNil(t, parsed)
	req := parsed.toUpsertRequest()
	require.Equal(t, "deadbeefdeadbeefdeadbeef", req.GetGatewayId())
	require.Equal(t, "edge-1", req.GetExternalId())
	require.Equal(t, "Roof GW", req.GetName())
	require.Equal(t, "0102030405060708", hex.EncodeToString(req.GetGatewayEui()))
	require.NotNil(t, req.Location)
	require.NotNil(t, req.Radios)
	require.Len(t, req.Radios.Values, 1)
}

func TestDetachedUpsertDoesNotBlockCaller(t *testing.T) {
	// Mirrors scheduleGatewayUpsert: fire-and-forget goroutine.
	var calls int32
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		atomic.AddInt32(&calls, 1)
		entered <- struct{}{}
		<-release
		_ = &api.RespGatewaySync{Applied: true}
		close(done)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("upsert did not start")
	}

	proxyWorkDone := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(proxyWorkDone)
	}()
	select {
	case <-proxyWorkDone:
	case <-time.After(2 * time.Second):
		t.Fatal("proxy work blocked by upsert")
	}

	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("upsert did not finish")
	}
	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}
