package main

import (
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
	lru "github.com/hashicorp/golang-lru/v2"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

// AnalyticsForwarder implements WSProxyEvents and batches analytics for push.
// Extraction is best-effort only; parse errors or unknown msgtypes never affect
// the WebSocket tunnel (the proxy forwards all messages before calling these handlers).
type AnalyticsForwarder struct {
	client       *client.Client
	config       ForwarderConfig
	proxy        *WSProxy
	metricsFrame *lru.Cache[string, *api.AnalyticsMetrics]
	drTables     map[string]DRTable
	drMu         sync.RWMutex
	dntxedCounts map[string]uint32 // per-gateway count of dntxed (downlinks put on air)
	dntxedMu     sync.Mutex
	isSending    bool
}

// CreateAnalyticsForwarder creates the forwarder and sets up the metrics cache.
func CreateAnalyticsForwarder(config ForwarderConfig, c *client.Client, proxy *WSProxy) *AnalyticsForwarder {
	f := &AnalyticsForwarder{
		config:       config,
		client:       c,
		proxy:        proxy,
		drTables:     make(map[string]DRTable),
		dntxedCounts: make(map[string]uint32),
	}
	f.metricsFrame, _ = lru.NewWithEvict(config.MaxWSStreams, f.handleEvict)
	return f
}

func (f *AnalyticsForwarder) handleEvict(key string, frame *api.AnalyticsMetrics) {
	f.flushDataFrame(key, frame)
}

func (f *AnalyticsForwarder) getMetricsFrame(gatewayKey string) *api.AnalyticsMetrics {
	if found, ok := f.metricsFrame.Get(gatewayKey); ok {
		return found
	}
	frame := &api.AnalyticsMetrics{}
	if eui := GatewayEUIFromRouter(gatewayKey); len(eui) == 8 {
		frame.GatewayEui = eui
	}
	if f.config.ServerSide {
		frame.Metrics = &api.AnalyticsInternalMetrics{GatewayIp: gatewayKey}
	}
	f.metricsFrame.Add(gatewayKey, frame)
	return frame
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
	if len(drTable) >= 16 {
		f.drTables[gatewayKey] = drTable[:16]
	}
}

// OnUpstreamMessage is called for each message from gateway to LNS.
func (f *AnalyticsForwarder) OnUpstreamMessage(gatewayKey string, raw []byte, isBinary bool) {
	if isBinary {
		return
	}
	frame := f.getMetricsFrame(gatewayKey)
	if frame.Metrics != nil {
		frame.Metrics.UpTxPackets++
	}
	msgtype, err := GetMsgtype(raw)
	if err != nil {
		return
	}
	switch msgtype {
	case "router_config":
		// Downstream message; we don't update up stats for it (it's LNS->GW). So router_config is not upstream.
		// So we should not see router_config in OnUpstreamMessage. So ignore.
		return
	case "version":
		return
	case "jreq":
		f.handleJreq(gatewayKey, raw, frame)
	case "updf":
		f.handleUpdf(gatewayKey, raw, frame)
	case "propdf":
		f.handlePropdf(gatewayKey, raw, frame)
	case "dntxed":
		f.dntxedMu.Lock()
		f.dntxedCounts[gatewayKey]++
		f.dntxedMu.Unlock()
		return
	case "timesync":
		return
	default:
		if frame.Metrics != nil {
			frame.Metrics.PktPUSH_DATA++
		}
	}
}

// OnDownstreamMessage is called for each message from LNS to gateway.
func (f *AnalyticsForwarder) OnDownstreamMessage(gatewayKey string, raw []byte, isBinary bool) {
	if isBinary {
		return
	}
	frame := f.getMetricsFrame(gatewayKey)
	if frame.Metrics != nil {
		frame.Metrics.DnRxPackets++
	}
	msgtype, err := GetMsgtype(raw)
	if err != nil {
		return
	}
	switch msgtype {
	case "router_config":
		cfg, err := ParseRouterConfig(raw)
		if err != nil {
			return
		}
		drTable := cfg.GetDRTable()
		if drTable != nil {
			f.SetDRTable(gatewayKey, drTable)
		}
		// Forward unchanged (no analytics from router_config)
	case "dnmsg":
		f.handleDnmsg(gatewayKey, raw, frame)
		if frame.Metrics != nil {
			frame.Metrics.PktPULL_RESP++
		}
	default:
		if frame.Metrics != nil {
			frame.Metrics.PktPULL_ACK++
		}
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

func (f *AnalyticsForwarder) bsToUplink(dr int, freq int, upinfo *UpInfo, drTable DRTable, now time.Time) *api.AnalyticsUplink {
	up := &api.AnalyticsUplink{
		RxWallTime:     now.UnixMicro(),
		Crc:            api.CRCStatus_OK,
		Modulation:     api.Modulation_LORA,
		CodingRate:     api.LoRaCodingRate_CR_UNKNOWN,
		RfChain:        0,
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
			up.DataRate = &api.AnalyticsUplink_DataRateFSK{DataRateFSK: uint32(bw)}
		} else {
			up.Modulation = api.Modulation_LORA
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
	dr := m.RX1DR
	if dr < 0 {
		dr = m.RX2DR
	}
	if dr < 0 {
		dr = m.DR
	}
	freq := m.RX1Freq
	if freq == 0 {
		freq = m.RX2Freq
	}
	if freq == 0 {
		freq = m.Freq
	}
	down := &api.AnalyticsDownlink{
		RxWallTime:     time.Now().UnixMilli(),
		TxGpsTime:      m.Gpstime,
		Frequency:     float32(freq) / 1e6,
		Fhdr:          fhdr,
		Size:          uint32(size),
		RfChain:       0,
		Modulation:    api.Modulation_LORA,
		CodingRate:    api.LoRaCodingRate_CR_UNKNOWN,
	}
	if drTable != nil && dr >= 0 && dr < len(drTable) {
		sf, bw := drTable[dr][0], drTable[dr][1]
		if sf == 0 {
			down.Modulation = api.Modulation_FSK
			down.DataRate = &api.AnalyticsDownlink_DataRateFSK{DataRateFSK: uint32(bw)}
		} else {
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

func (f *AnalyticsForwarder) connect() {
	for {
		log.Debugf("Connecting to: %s", f.config.Endpoint)
		err := f.client.Connect()
		if err != nil {
			log.Warnf("Could not connect to analytics endpoint: %s", err.Error())
			time.Sleep(10 * time.Second)
			continue
		}
		go f.main()
		return
	}
}

func (f *AnalyticsForwarder) main() {
	log.Info("Connected to kudzu analytics")
	for {
		time.Sleep(time.Duration(f.config.FlushInterval) * time.Second)
		if !f.isSending && f.hasData() {
			f.flushData()
		}
	}
}

func (f *AnalyticsForwarder) hasData() bool {
	return f.queueSize() > 0
}

func (f *AnalyticsForwarder) queueSize() int {
	var total int
	for _, frame := range f.metricsFrame.Values() {
		total += len(frame.Uplinks) + len(frame.Downlinks) + len(frame.Stats)
		if frame.Metrics != nil {
			if frame.Metrics.DnRxPackets > 0 || frame.Metrics.DnTxPackets > 0 ||
				frame.Metrics.UpRxPackets > 0 || frame.Metrics.UpTxPackets > 0 {
				if total == 0 {
					total++
				}
			}
		}
	}
	return total
}

func (f *AnalyticsForwarder) flushDataFrame(gatewayKey string, frame *api.AnalyticsMetrics) {
	copy := proto.Clone(frame).(*api.AnalyticsMetrics)
	f.dntxedMu.Lock()
	dntxed := f.dntxedCounts[gatewayKey]
	f.dntxedCounts[gatewayKey] = 0
	f.dntxedMu.Unlock()
	// Basic Station has no native stat message; derive AnalyticsStat from proxy counters.
	if copy.Metrics != nil {
		copy.Stats = append(copy.Stats, f.syntheticStatFromMetrics(frame.Metrics, dntxed))
	} else if len(copy.Uplinks) > 0 || len(copy.Downlinks) > 0 || dntxed > 0 {
		copy.Stats = append(copy.Stats, f.syntheticStatFromBatch(len(copy.Uplinks), len(copy.Downlinks), dntxed))
	}
	frame.Downlinks = nil
	frame.Uplinks = nil
	frame.Stats = nil
	if frame.Metrics != nil {
		frame.Metrics.DnRxPackets = 0
		frame.Metrics.DnTxPackets = 0
		frame.Metrics.UpRxPackets = 0
		frame.Metrics.UpTxPackets = 0
		frame.Metrics.PktPULL_ACK = 0
		frame.Metrics.PktPULL_DATA = 0
		frame.Metrics.PktPULL_RESP = 0
		frame.Metrics.PktPUSH_ACK = 0
		frame.Metrics.PktPUSH_DATA = 0
		frame.Metrics.PktTX_ACK = 0
	}
	if err := f.client.PushMetrics(copy); err != nil {
		log.Warnf("Unable to push metrics: %s", err.Error())
	}
}

// syntheticStatFromMetrics builds an AnalyticsStat from AnalyticsInternalMetrics (server-side).
func (f *AnalyticsForwarder) syntheticStatFromMetrics(m *api.AnalyticsInternalMetrics, dntxed uint32) *api.AnalyticsStat {
	s := &api.AnalyticsStat{
		GwTime:            time.Now().UnixMilli(),
		RxPackets:         m.UpTxPackets,
		RxWithValidPhyCRC: m.UpTxPackets,
		RxForwarded:       m.UpTxPackets,
		TxReceived:        m.DnRxPackets,
		TxEmitted:         dntxed,
		IsGauge:           f.config.GaugeStat,
	}
	if m.DnRxPackets > 0 {
		s.RxAckr = 100 * float32(dntxed) / float32(m.DnRxPackets)
	}
	return s
}

// syntheticStatFromBatch builds an AnalyticsStat from batch counts (client-side, no Metrics).
func (f *AnalyticsForwarder) syntheticStatFromBatch(uplinks, downlinks int, dntxed uint32) *api.AnalyticsStat {
	s := &api.AnalyticsStat{
		GwTime:            time.Now().UnixMilli(),
		RxPackets:         uint32(uplinks),
		RxWithValidPhyCRC: uint32(uplinks),
		RxForwarded:       uint32(uplinks),
		TxReceived:        uint32(downlinks),
		TxEmitted:         dntxed,
		IsGauge:           f.config.GaugeStat,
	}
	if downlinks > 0 {
		s.RxAckr = 100 * float32(dntxed) / float32(downlinks)
	}
	return s
}

func (f *AnalyticsForwarder) flushData() {
	f.isSending = true
	for _, key := range f.metricsFrame.Keys() {
		if frame, ok := f.metricsFrame.Get(key); ok {
			f.flushDataFrame(key, frame)
		}
	}
	f.isSending = false
}

// StartAndWait connects to analytics and runs the flush loop; blocks forever.
func (f *AnalyticsForwarder) StartAndWait() {
	go f.connect()
	ch := make(chan struct{})
	<-ch
}

// GatewayEUIFromRouter parses router string (16 hex chars, optional separators) to 8-byte EUI.
func GatewayEUIFromRouter(router string) []byte {
	const hexChars = "0123456789abcdefABCDEF"
	s := router
	for _, sep := range []string{"-", ":", " "} {
		s = strings.ReplaceAll(s, sep, "")
	}
	if len(s) != 16 {
		return nil
	}
	eui, err := hex.DecodeString(s)
	if err != nil || len(eui) != 8 {
		return nil
	}
	return eui
}
