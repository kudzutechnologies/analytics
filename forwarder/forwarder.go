package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type AnalyticsForwarder struct {
	client       *client.Client
	config       ForwarderConfig
	proxy        *UDPProxy
	metricsFrame *lru.Cache[string, *api.AnalyticsMetrics]
	isSending    bool
}

func CreateAnalyticsForwarder(config ForwarderConfig, client *client.Client, proxy *UDPProxy) *AnalyticsForwarder {
	inst := &AnalyticsForwarder{
		config:    config,
		client:    client,
		proxy:     proxy,
		isSending: false,
	}
	inst.metricsFrame, _ = lru.NewWithEvict(config.MaxUDPStreams, inst.handleEvict)
	return inst
}

func (f *AnalyticsForwarder) StartAndWait() {
	// Connect to the analytics endpoint from another thread
	// because it might not be available right away
	go f.connect()

	// Never return
	ch := make(chan bool)
	<-ch
}

func (f *AnalyticsForwarder) handleEvict(key string, frame *api.AnalyticsMetrics) {
	f.flushDataFrame(frame)
}

func (f *AnalyticsForwarder) getMetricsFrame(localEp *net.UDPAddr) *api.AnalyticsMetrics {
	key := fmt.Sprintf("%s", localEp.IP.String())
	if found, ok := f.metricsFrame.Get(key); ok {
		return found
	}

	found := &api.AnalyticsMetrics{}

	// Include stats only on the server-side
	if f.config.ServerSide {
		found.Metrics = &api.AnalyticsInternalMetrics{
			GatewayIp: localEp.String(),
		}
	}

	f.metricsFrame.Add(key, found)
	return found
}

func (f *AnalyticsForwarder) connect() {
	// Keep trying until the analytics client is connected
	for {
		err := f.client.Connect()
		if err != nil {
			log.Warnf("Could not connect to analytics endpoint: %s", err.Error())
		} else {
			// We are ready, start main thread
			go f.main()
			return
		}

		// Back-off and try to connect again
		time.Sleep(10 * time.Second)
	}
}

func (f *AnalyticsForwarder) main() {
	log.Info("Connected to kudzu analytics")

	// Start receiving traffic from the UDP proxy
	f.proxy.SetEventHandler(f)

	// Periodically flush data waiting in the egress queue
	for {
		log.Debugf("Sleeping for %d sec", f.config.FlushInterval)
		time.Sleep(time.Second * time.Duration(f.config.FlushInterval))
		log.Debugf("Queue size=%d, isSending=%v", f.queueSize(), f.isSending)
		if !f.isSending && f.hasData() {
			f.flushData()
		}
	}
}

func (f *AnalyticsForwarder) hasData() bool {
	return f.queueSize() > 0
}

func (f *AnalyticsForwarder) queueSize() int {
	var total int = 0
	for _, f := range f.metricsFrame.Values() {
		sum := len(f.Uplinks) +
			len(f.Downlinks) +
			len(f.Stats)

		if f.Metrics.DnRxPackets > 0 || f.Metrics.DnTxPackets > 0 ||
			f.Metrics.UpRxPackets > 0 || f.Metrics.UpTxPackets > 0 {
			if sum == 0 {
				sum++
			}
		}

		total += sum
	}

	return total
}

func (f *AnalyticsForwarder) flushDataFrame(frame *api.AnalyticsMetrics) {
	// Copy frame to allow it to be re-used while sending
	frameCopy := proto.Clone(frame).(*api.AnalyticsMetrics)

	// Reset frame
	frame.Downlinks = nil
	frame.Uplinks = nil
	frame.Stats = nil
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

	// Push a copy
	err := f.client.PushMetrics(frameCopy)
	if err != nil {
		log.Warnf("Unable to push metrics: %s", err.Error())
	}
}

func (f *AnalyticsForwarder) flushData() {
	f.isSending = true
	log.Debugf("Flushing %d frames in %d gateways", f.queueSize(), f.metricsFrame.Len())

	// Flush data
	for _, sendFrame := range f.metricsFrame.Values() {
		f.flushDataFrame(sendFrame)
	}

	f.isSending = false
}

func (f *AnalyticsForwarder) UpLocalData(data []byte, localEp *net.UDPAddr) {
	frame := f.getMetricsFrame(localEp)
	frame.Metrics.UpTxPackets += 1
	if SemtechUDPIsUplink(data) {
		f.handleUplink(data, localEp, frame)
	} else if SemtechUDPIsDownlink(data) {
		f.handleDownlink(data, localEp, frame)
	}
}

func (f *AnalyticsForwarder) UpRemoteData(data []byte, localEp *net.UDPAddr) {
	frame := f.getMetricsFrame(localEp)
	frame.Metrics.UpRxPackets += 1

	if SemtechUDPIsUplink(data) {
		f.handleUplink(data, localEp, frame)
	} else if SemtechUDPIsDownlink(data) {
		f.handleDownlink(data, localEp, frame)
	}
}

func (f *AnalyticsForwarder) DnLocalData(data []byte, localEp *net.UDPAddr) {
	frame := f.getMetricsFrame(localEp)
	frame.Metrics.DnTxPackets += 1

	if SemtechUDPIsUplink(data) {
		f.handleUplink(data, localEp, frame)
	} else if SemtechUDPIsDownlink(data) {
		f.handleDownlink(data, localEp, frame)
	}
}

func (f *AnalyticsForwarder) DnRemoteData(data []byte, localEp *net.UDPAddr) {
	frame := f.getMetricsFrame(localEp)
	frame.Metrics.DnRxPackets += 1

	if SemtechUDPIsUplink(data) {
		f.handleUplink(data, localEp, frame)
	} else if SemtechUDPIsDownlink(data) {
		f.handleDownlink(data, localEp, frame)
	}
}

func (f *AnalyticsForwarder) incPktStat(frame *SemtechUDPMessage, metricsFrame *api.AnalyticsMetrics) {
	switch frame.Kind {
	case PUSH_DATA:
		metricsFrame.Metrics.PktPUSH_DATA += 1
	case PUSH_ACK:
		metricsFrame.Metrics.PktPUSH_ACK += 1

	case PULL_DATA:
		metricsFrame.Metrics.PktPULL_DATA += 1
	case PULL_RESP:
		metricsFrame.Metrics.PktPULL_RESP += 1

	case PULL_ACK:
		metricsFrame.Metrics.PktPULL_ACK += 1
	case TX_ACK:
		metricsFrame.Metrics.PktTX_ACK += 1
	}
}

func (f *AnalyticsForwarder) handleUplink(data []byte, localEp *net.UDPAddr, metricsFrame *api.AnalyticsMetrics) {
	log.Debugf("Handling uplink frame from %s: %s", localEp.String(), hex.EncodeToString(data))
	frame, err := DecodeMessage(data, len(data), localEp, time.Now(), []string{})
	if err != nil {
		log.Warnf("Could not handle uplink: %s", err.Error())
	} else {
		f.incPktStat(frame, metricsFrame)
		eui := frame.GatewayEUI()
		if eui != nil {
			log.Debugf("Gateway EUI: %s, Token: %04x", hex.EncodeToString(eui), frame.Token)

			// Configure gateway
			metricsFrame.GatewayEui = eui
			if !f.config.ServerSide {
				metricsFrame.GatewayId = f.config.GatewayId
			}

			// Convert uplinks
			rx, err := frame.GetAllRxPkt()
			if err == nil && rx != nil {
				for _, r := range rx {
					pkt := f.convertRxPkt(&r)
					log.Debugf("Got uplink: %+v", pkt)
					metricsFrame.Uplinks = append(metricsFrame.Uplinks, pkt)
				}
			}

			// Convert stat
			stat, err := frame.GetStatMsg()
			if err == nil && stat != nil {
				pkt := f.convertStatPkt(stat)
				log.Debugf("Got stat: %+v", pkt)
				metricsFrame.Stats = append(metricsFrame.Stats, pkt)
			}
		} else {
			log.Debugf("No EUI in the frame")
		}
	}

	log.Debugf("Queue size=%d", f.queueSize())
}

func (f *AnalyticsForwarder) handleDownlink(data []byte, localEp *net.UDPAddr, metricsFrame *api.AnalyticsMetrics) {
	log.Debugf("Handling downlink frame from %s: %s", localEp.String(), hex.EncodeToString(data))
	frame, err := DecodeMessage(data, len(data), localEp, time.Now(), []string{})
	if err != nil {
		log.Warnf("Could not handle downlink: %s", err.Error())
	} else {
		f.incPktStat(frame, metricsFrame)

		eui := frame.GatewayEUI()
		log.Debugf("Gateway EUI: %s, Token: %04x", hex.EncodeToString(eui), frame.Token)
		log.Debugf("Pair Gateway EUI: %s", hex.EncodeToString(metricsFrame.GatewayEui))
		if eui != nil {
			metricsFrame.GatewayEui = eui
		}

		// Convert downlinks
		tx, err := frame.GetTxPacket()
		if err == nil && tx != nil {
			log.Debugf("Got downlink: %+v", tx)
			metricsFrame.Downlinks = append(metricsFrame.Downlinks, f.convertTxPkt(tx))
		}
	}

	log.Debugf("Queue size=%d", f.queueSize())
}

////////////////////////////////////////////////////////////////////////////////////
// Translation Utilities
////////////////////////////////////////////////////////////////////////////////////

func parseCodingRate(cr string) api.LoRaCodingRate {
	switch cr {
	case "off":
		return api.LoRaCodingRate_CR_OFF
	case "4/5":
		return api.LoRaCodingRate_CR_4_5
	case "4/6":
		return api.LoRaCodingRate_CR_4_6
	case "4/7":
		return api.LoRaCodingRate_CR_4_7
	case "4/8":
		return api.LoRaCodingRate_CR_4_8
	case "4/9":
		return api.LoRaCodingRate_CR_4_9
	case "4/10":
		return api.LoRaCodingRate_CR_4_10
	case "4/11":
		return api.LoRaCodingRate_CR_4_11
	case "4/12":
		return api.LoRaCodingRate_CR_4_12
	case "4/13":
		return api.LoRaCodingRate_CR_4_13
	case "4/14":
		return api.LoRaCodingRate_CR_4_14
	case "4/15":
		return api.LoRaCodingRate_CR_4_15
	case "4/16":
		return api.LoRaCodingRate_CR_4_16
	}

	return api.LoRaCodingRate_CR_UNKNOWN
}

func parseCrcStat(stat int) api.CRCStatus {
	switch stat {
	case 1:
		return api.CRCStatus_OK
	case -1:
		return api.CRCStatus_FAIL
	}

	return api.CRCStatus_MISSING
}

func parseModu(modu string) api.Modulation {
	switch modu {
	case "LORA":
		return api.Modulation_LORA
	case "FSK":
		return api.Modulation_FSK
	}

	return api.Modulation_UNKNOWN
}

func parseSF(sf string) api.LoRaSF {
	switch sf {
	case "7":
		return api.LoRaSF_SF7
	case "8":
		return api.LoRaSF_SF8
	case "9":
		return api.LoRaSF_SF9
	case "10":
		return api.LoRaSF_SF10
	case "11":
		return api.LoRaSF_SF11
	case "12":
		return api.LoRaSF_SF12
	}

	return api.LoRaSF_SF_UNKNOWN
}

func parseBW(bw string) api.LoRaBW {
	switch bw {
	case "125":
		return api.LoRaBW_BW_125k
	case "250":
		return api.LoRaBW_BW_250k
	case "500":
		return api.LoRaBW_BW_500k
	}

	return api.LoRaBW_BW_UNKNOWN
}

func parseDataLoRaRate(dataRate string) *api.LoRaDataRate {
	var lora api.LoRaDataRate
	// EG. 'SF7BW125'
	bw := strings.Index(dataRate, "BW")
	if bw == -1 {
		log.Warnf("Unparsable data rate '%s'", dataRate)
	} else {
		lora.SpreadingFactor = parseSF(dataRate[2:bw])
		lora.Bandwidth = parseBW(dataRate[bw+2:])
	}
	return &lora
}

func (f *AnalyticsForwarder) convertRxPkt(in *SemtechUDPRxPkt) *api.AnalyticsUplink {
	var out api.AnalyticsUplink
	var tm time.Time

	err := json.Unmarshal([]byte(in.Time), &tm)
	if err == nil {
		out.RxWallTime = tm.UnixMicro()
	}

	out.RxFinishedTime = in.Tmst
	out.RxGpsTime = in.Tmms
	out.Frequency = in.Frequency
	out.RfChain = uint32(in.RfChain)
	out.CodingRate = parseCodingRate(in.CodingRate)
	out.Crc = parseCrcStat(in.Stat)
	out.Modulation = parseModu(in.Modulation)

	switch in.Modulation {
	case "LORA":
		out.DataRate = &api.AnalyticsUplink_DataRateLoRa{
			DataRateLoRa: parseDataLoRaRate(in.DataRate),
		}
	case "FSK":
		val, err := strconv.Atoi(in.DataRate)
		if err == nil {
			out.DataRate = &api.AnalyticsUplink_DataRateFSK{
				DataRateFSK: uint32(val),
			}
		}
	}

	if len(in.RSig) > 0 {
		for _, ant := range in.RSig {
			oAnt := &api.AnalyticsUplinkAntenna{
				Antenna: int32(ant.Ant),
				IfChan:  int32(ant.Chan),
				RSSIC:   int32(ant.RSSIC),
				LSNR:    float32(ant.LSNR),
				ETime:   ant.ETime,
				FTime:   ant.FTime,
				Foff:    ant.FOff,
			}

			if ant.RSSIS != nil {
				var value int32 = int32(*ant.RSSIS)
				oAnt.RSSIS = &value
			}
			if ant.RSSISD != nil {
				var value int32 = int32(*ant.RSSISD)
				oAnt.RSSISD = &value
			}

			out.Ant = append(out.Ant, oAnt)
		}
	} else {
		out.Ant = append(out.Ant, &api.AnalyticsUplinkAntenna{
			Antenna: 0,
			IfChan:  int32(in.Channel),
			RSSIC:   int32(in.Rssi),
			LSNR:    in.Lsnr,
		})
	}
	out.Size = uint32(in.Size)

	data, err := base64.StdEncoding.DecodeString(in.Data)
	if err == nil {
		fhdrLen := GetLoRaWANHeaderLen(data)
		out.Fhdr = data[0:fhdrLen]
		api.ComputeUniqueIdUp(&out, data)
	}

	return &out
}

func (f *AnalyticsForwarder) convertStatPkt(in *SemtechUDPStat) *api.AnalyticsStat {
	var out api.AnalyticsStat
	var tm time.Time

	err := json.Unmarshal([]byte(in.Time), &tm)
	if err == nil {
		out.GwTime = tm.UnixMilli()
	}

	out.GwLatitude = in.Lati
	out.GwLongitude = in.Long
	out.GwAltitude = in.Alti
	out.RxPackets = uint32(in.RxNb)
	out.RxWithValidPhyCRC = uint32(in.RxOk)
	out.RxForwarded = uint32(in.RxFw)
	out.RxAckr = in.Ackr
	out.TxReceived = uint32(in.DwnB)
	out.TxEmitted = uint32(in.TxNb)

	out.IsGauge = f.config.GaugeStat

	return &out
}

func (f *AnalyticsForwarder) convertTxPkt(in *SemtechUDPTxPkt) *api.AnalyticsDownlink {
	var out api.AnalyticsDownlink

	out.TxTime = in.Tmst
	out.TxGpsTime = in.Tmms
	out.FskFreqDev = in.Fdev
	out.Frequency = in.Frequency
	out.Channel = 0
	out.RfChain = uint32(in.RfChain)
	out.Power = in.Power
	out.Modulation = parseModu(in.Modulation)
	out.CodingRate = parseCodingRate(in.CodingRate)

	switch in.Modulation {
	case "LORA":
		out.DataRate = &api.AnalyticsDownlink_DataRateLoRa{
			DataRateLoRa: parseDataLoRaRate(in.DataRate),
		}
	case "FSK":
		val, err := strconv.Atoi(in.DataRate)
		if err == nil {
			out.DataRate = &api.AnalyticsDownlink_DataRateFSK{
				DataRateFSK: uint32(val),
			}
		}
	}

	out.InvertPolarity = in.Ipol
	out.Immediately = in.Imme
	out.RfPreamble = uint32(in.Prea)
	out.Size = uint32(in.Size)
	out.NoCrc = in.NoCRC
	out.RxWallTime = time.Now().UnixMilli()

	data, err := base64.StdEncoding.DecodeString(in.Data)
	if err == nil {
		fhdrLen := GetLoRaWANHeaderLen(data)
		out.Fhdr = data[0:fhdrLen]
		api.ComputeUniqueIdDown(&out, data)
	}

	return &out
}
