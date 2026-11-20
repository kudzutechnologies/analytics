package main

import (
	"encoding/hex"
	"sync"
	"time"

	"github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
	log "github.com/sirupsen/logrus"
)

// AnalyticsForwarder implements WSProxyEvents and batches analytics for push.
// Extraction is best-effort only; parse errors or unknown msgtypes never affect
// the WebSocket tunnel (the proxy forwards all messages before calling these handlers).
type AnalyticsForwarder struct {
	config    ForwarderConfig
	backend   analyticsTransport
	queue     *deliveryQueue
	collectMu sync.Mutex
	stopped   bool
	drTables  map[string]DRTable
	drMu      sync.RWMutex
	counters  map[string]*radioCounters
	startOnce sync.Once
	cancel    func()
	done      chan struct{}
}
type radioCounters struct{ up, down, emitted uint32 }

func CreateAnalyticsForwarder(config ForwarderConfig, c *client.Client, proxy *WSProxy) *AnalyticsForwarder {
	if config.MaxBatches < 1 {
		config.MaxBatches = 2
	}
	if config.MaxMessages < 1 {
		config.MaxMessages = 32
	}
	if config.MaxBufferBytes < 1024 {
		config.MaxBufferBytes = 65536
	}
	if config.MaxWSStreams < 1 {
		config.MaxWSStreams = 1
	}
	if config.FlushInterval < 1 {
		config.FlushInterval = 10
	}
	if config.ConnectTimeout < 1 {
		config.ConnectTimeout = 5
	}
	if config.RequestTimeout < 1 {
		config.RequestTimeout = 5
	}
	if config.MaxReconnectBackoff < 1 {
		config.MaxReconnectBackoff = 60
	}
	f := &AnalyticsForwarder{config: config, drTables: make(map[string]DRTable), counters: make(map[string]*radioCounters), done: make(chan struct{})}
	if c != nil {
		f.backend = c
	}
	f.queue = newDeliveryQueue(config)
	return f
}
func (f *AnalyticsForwarder) frame(key string) *api.AnalyticsMetrics {
	m := &api.AnalyticsMetrics{GatewayEui: GatewayEUIFromRouter(key)}
	if !f.config.ServerSide {
		m.GatewayId = f.config.GatewayId
	}
	return m
}
func (f *AnalyticsForwarder) counter(key string) *radioCounters {
	if c := f.counters[key]; c != nil {
		return c
	}
	if len(f.counters) >= f.config.MaxWSStreams {
		return nil
	}
	c := &radioCounters{}
	f.counters[key] = c
	return c
}

func (f *AnalyticsForwarder) getDRTable(gatewayKey string) DRTable {
	f.drMu.RLock()
	defer f.drMu.RUnlock()
	return f.drTables[gatewayKey]
}

// SetDRTable stores the DR table for a gateway (called when router_config is received).
func (f *AnalyticsForwarder) SetDRTable(gatewayKey string, drTable DRTable) {
	f.drMu.Lock()
	defer f.drMu.Unlock()
	if len(drTable) >= 16 && (f.drTables[gatewayKey] != nil || len(f.drTables) < f.config.MaxWSStreams) {
		f.drTables[gatewayKey] = drTable[:16]
	}
}

// OnUpstreamMessage is called for each message from gateway to LNS.
func (f *AnalyticsForwarder) OnUpstreamMessage(key string, raw []byte, isBinary bool) {
	if isBinary {
		return
	}
	f.collectMu.Lock()
	defer f.collectMu.Unlock()
	if f.stopped {
		return
	}
	kind, err := GetMsgtype(raw)
	if err != nil {
		return
	}
	m := f.frame(key)
	switch kind {
	case "jreq":
		f.handleJreq(key, raw, m)
	case "updf":
		f.handleUpdf(key, raw, m)
	case "propdf":
		f.handlePropdf(key, raw, m)
	case "dntxed":
		if _, err := ParseDntxed(raw); err == nil {
			if c := f.counter(key); c != nil {
				c.emitted++
			}
		}
		return
	default:
		return
	}
	if len(m.Uplinks) > 0 {
		if c := f.counter(key); c != nil {
			c.up++
		}
		f.queue.add(key, m, true)
	}
}
func (f *AnalyticsForwarder) OnDownstreamMessage(key string, raw []byte, isBinary bool) {
	if isBinary {
		return
	}
	f.collectMu.Lock()
	defer f.collectMu.Unlock()
	if f.stopped {
		return
	}
	kind, err := GetMsgtype(raw)
	if err != nil {
		return
	}
	if kind == "router_config" {
		cfg, err := ParseRouterConfig(raw)
		if err == nil {
			f.SetDRTable(key, cfg.GetDRTable())
		}
		return
	}
	if kind != "dnmsg" {
		return
	}
	m := f.frame(key)
	f.handleDnmsg(key, raw, m)
	if len(m.Downlinks) > 0 {
		if c := f.counter(key); c != nil {
			c.down++
		}
		f.queue.add(key, m, true)
	}
}

func (f *AnalyticsForwarder) handleJreq(gatewayKey string, raw []byte, metricsFrame *api.AnalyticsMetrics) {
	m, err := ParseJreq(raw)
	if err != nil {
		return
	}
	drTable := f.getDRTable(gatewayKey)
	up := f.bsToUplink(m.DR, m.Freq, m.Upinfo, drTable, time.Now())
	if up == nil {
		return
	}
	fhdr, fullPayload, err := ReconstructJreqPayload(m)
	if err != nil {
		log.Debugf("jreq reconstruct: %v", err)
		return
	}
	up.Fhdr = fhdr
	up.Size = uint32(len(fullPayload))
	ComputeUniqueIdUpBS(up, fullPayload)
	if !f.config.ServerSide {
		metricsFrame.GatewayId = f.config.GatewayId
	}
	metricsFrame.Uplinks = append(metricsFrame.Uplinks, up)
}

func (f *AnalyticsForwarder) handleUpdf(gatewayKey string, raw []byte, metricsFrame *api.AnalyticsMetrics) {
	m, err := ParseUpdf(raw)
	if err != nil {
		return
	}
	drTable := f.getDRTable(gatewayKey)
	up := f.bsToUplink(m.DR, m.Freq, m.Upinfo, drTable, time.Now())
	if up == nil {
		return
	}
	fhdr, fullPayload, err := ReconstructUpdfPayload(m)
	if err != nil {
		log.Debugf("updf reconstruct: %v", err)
		return
	}
	up.Fhdr = fhdr
	up.Size = uint32(len(fullPayload))
	ComputeUniqueIdUpBS(up, fullPayload)
	if !f.config.ServerSide {
		metricsFrame.GatewayId = f.config.GatewayId
	}
	metricsFrame.Uplinks = append(metricsFrame.Uplinks, up)
}

func (f *AnalyticsForwarder) handlePropdf(gatewayKey string, raw []byte, metricsFrame *api.AnalyticsMetrics) {
	m, err := ParsePropdf(raw)
	if err != nil {
		return
	}
	drTable := f.getDRTable(gatewayKey)
	up := f.bsToUplink(m.DR, m.Freq, m.Upinfo, drTable, time.Now())
	if up == nil {
		return
	}
	fhdr, fullPayload, err := ReconstructPropdfPayload(m)
	if err != nil {
		log.Debugf("propdf reconstruct: %v", err)
		return
	}
	up.Fhdr = fhdr
	up.Size = uint32(len(fullPayload))
	ComputeUniqueIdUpBS(up, fullPayload)
	if !f.config.ServerSide {
		metricsFrame.GatewayId = f.config.GatewayId
	}
	metricsFrame.Uplinks = append(metricsFrame.Uplinks, up)
}

func (f *AnalyticsForwarder) bsToUplink(dr int, freq int64, upinfo *UpInfo, drTable DRTable, now time.Time) *api.AnalyticsUplink {
	if dr < 0 || dr > 15 || freq <= 0 || freq > 4294967295 {
		return nil
	}
	up := &api.AnalyticsUplink{
		RxWallTime: now.UnixMicro(),
		Crc:        api.CRCStatus_OK,
		Modulation: api.Modulation_UNKNOWN,
		CodingRate: api.LoRaCodingRate_CR_UNKNOWN,
		RfChain:    0,
	}
	if upinfo != nil {
		up.RxGpsTime = upinfo.Gpstime
		up.RxFinishedTime = upinfo.Xtime
		up.Ant = []*api.AnalyticsUplinkAntenna{{
			Antenna: 0,
			RSSIC:   int32(upinfo.Rssi),
			LSNR:    float32(upinfo.Snr),
		}}
	}
	up.Frequency = float32(freq) / 1e6
	if drTable != nil && dr >= 0 && dr < len(drTable) {
		sf, bw, _ := drTable[dr][0], drTable[dr][1], drTable[dr][2]
		if sf == 0 {
			up.Modulation = api.Modulation_FSK
			up.CodingRate = api.LoRaCodingRate_CR_OFF
			up.DataRate = &api.AnalyticsUplink_DataRateFSK{DataRateFSK: uint32(50000)}
		} else if sf >= 7 && sf <= 12 && drToBW(bw) != api.LoRaBW_BW_UNKNOWN {
			up.Modulation = api.Modulation_LORA
			up.CodingRate = api.LoRaCodingRate_CR_4_5
			up.DataRate = &api.AnalyticsUplink_DataRateLoRa{
				DataRateLoRa: &api.LoRaDataRate{
					SpreadingFactor: drToSF(sf),
					Bandwidth:       drToBW(bw),
				},
			}
		}
	}
	return up
}

func (f *AnalyticsForwarder) handleDnmsg(gatewayKey string, raw []byte, metricsFrame *api.AnalyticsMetrics) {
	m, err := ParseDnmsg(raw)
	if err != nil {
		return
	}
	rawPdu, fhdr, size, err := DecodeDnmsgPdu(m.Pdu)
	if err != nil {
		return
	}
	drTable := f.getDRTable(gatewayKey)
	dr := -1
	if m.RX1DR != nil {
		dr = *m.RX1DR
	} else if m.RX2DR != nil {
		dr = *m.RX2DR
	} else if m.DR != nil {
		dr = *m.DR
	}
	if dr > 15 || dr < -1 {
		return
	}
	freq := m.RX1Freq
	if freq == 0 {
		freq = m.RX2Freq
	}
	if freq == 0 {
		freq = m.Freq
	}
	if freq <= 0 || freq > 4294967295 {
		return
	}
	down := &api.AnalyticsDownlink{
		RxWallTime: time.Now().UnixMilli(),
		TxGpsTime:  m.Gpstime,
		Frequency:  float32(freq) / 1e6,
		Fhdr:       fhdr,
		Size:       uint32(size),
		RfChain:    0,
		Modulation: api.Modulation_UNKNOWN,
		CodingRate: api.LoRaCodingRate_CR_UNKNOWN,
	}
	if drTable != nil && dr >= 0 && dr < len(drTable) {
		sf, bw := drTable[dr][0], drTable[dr][1]
		if sf == 0 {
			down.Modulation = api.Modulation_FSK
			down.CodingRate = api.LoRaCodingRate_CR_OFF
			down.DataRate = &api.AnalyticsDownlink_DataRateFSK{DataRateFSK: uint32(50000)}
		} else if sf >= 7 && sf <= 12 && drToBW(bw) != api.LoRaBW_BW_UNKNOWN {
			down.Modulation = api.Modulation_LORA
			down.CodingRate = api.LoRaCodingRate_CR_4_5
			down.DataRate = &api.AnalyticsDownlink_DataRateLoRa{
				DataRateLoRa: &api.LoRaDataRate{
					SpreadingFactor: drToSF(sf),
					Bandwidth:       drToBW(bw),
				},
			}
		}
	}
	ComputeUniqueIdDownBS(down, rawPdu)
	if !f.config.ServerSide {
		metricsFrame.GatewayId = f.config.GatewayId
	}
	metricsFrame.Downlinks = append(metricsFrame.Downlinks, down)
}

func drToSF(sf int) api.LoRaSF {
	switch sf {
	case 7:
		return api.LoRaSF_SF7
	case 8:
		return api.LoRaSF_SF8
	case 9:
		return api.LoRaSF_SF9
	case 10:
		return api.LoRaSF_SF10
	case 11:
		return api.LoRaSF_SF11
	case 12:
		return api.LoRaSF_SF12
	}
	return api.LoRaSF_SF_UNKNOWN
}

func drToBW(bw int) api.LoRaBW {
	switch bw {
	case 125:
		return api.LoRaBW_BW_125k
	case 250:
		return api.LoRaBW_BW_250k
	case 500:
		return api.LoRaBW_BW_500k
	}
	return api.LoRaBW_BW_UNKNOWN
}

func (f *AnalyticsForwarder) flushData() {
	f.collectMu.Lock()
	defer f.collectMu.Unlock()
	for key, c := range f.counters {
		if c.up == 0 && c.down == 0 && c.emitted == 0 {
			continue
		}
		m := f.frame(key)
		m.Stats = []*api.AnalyticsStat{{GwTime: time.Now().UnixMilli(), RxPackets: c.up, RxWithValidPhyCRC: c.up, RxForwarded: c.up, TxReceived: c.down, TxEmitted: c.emitted, IsGauge: f.config.GaugeStat}}
		f.queue.add(key, m, false)
		*c = radioCounters{}
	}
	f.queue.seal(false)
}
func (f *AnalyticsForwarder) hasData() bool  { return f.queue.snapshot().Batches > 0 }
func (f *AnalyticsForwarder) queueSize() int { return f.queue.snapshot().Messages }
func (f *AnalyticsForwarder) StartAndWait()  { f.Start(); <-f.done }
func (f *AnalyticsForwarder) OnSessionClosed(key string) {
	f.collectMu.Lock()
	if c := f.counters[key]; c != nil && (c.up != 0 || c.down != 0 || c.emitted != 0) {
		m := f.frame(key)
		m.Stats = []*api.AnalyticsStat{{GwTime: time.Now().UnixMilli(), RxPackets: c.up, RxWithValidPhyCRC: c.up, RxForwarded: c.up, TxReceived: c.down, TxEmitted: c.emitted, IsGauge: f.config.GaugeStat}}
		f.queue.add(key, m, false)
		f.queue.seal(false)
	}
	delete(f.counters, key)
	f.collectMu.Unlock()
	f.drMu.Lock()
	delete(f.drTables, key)
	f.drMu.Unlock()
}

// GatewayEUIFromRouter parses router string (16 hex chars, optional separators) to 8-byte EUI.
func GatewayEUIFromRouter(router string) []byte {
	id, err := ParseGatewayID(router)
	if err != nil {
		return nil
	}
	eui, _ := hex.DecodeString(id)
	return eui
}
