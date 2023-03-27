package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
	log "github.com/sirupsen/logrus"
)

type AnalyticsForwarder struct {
	client      *client.Client
	config      ForwarderConfig
	proxy       *UDPProxy
	uplinkFrame *api.AnalyticsMetrics
	isSending   bool
}

func CreateAnalyticsForwarder(config ForwarderConfig, client *client.Client, proxy *UDPProxy) *AnalyticsForwarder {
	return &AnalyticsForwarder{
		config:      config,
		client:      client,
		proxy:       proxy,
		uplinkFrame: &api.AnalyticsMetrics{},
		isSending:   false,
	}
}

func (f *AnalyticsForwarder) StartAndWait() {
	// Connect to the analytics endpoint from another thread
	// because it might not be available right away
	go f.connect()

	// Never return
	ch := make(chan bool)
	<-ch
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
	f.proxy.SetListener(f)

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
	return len(f.uplinkFrame.Uplinks) +
		len(f.uplinkFrame.Downlinks) +
		len(f.uplinkFrame.Stats)
}

func (f *AnalyticsForwarder) flushData() {
	f.isSending = true
	log.Debugf("Flushing %d frames", f.queueSize())

	// Swap frames to let the current buffer to keep being filled
	sendFrame := f.uplinkFrame
	f.uplinkFrame = &api.AnalyticsMetrics{}

	// Flush data
	sendFrame.GatewayId = f.config.GatewayId
	err := f.client.PushMetrics(sendFrame)
	if err != nil {
		log.Warnf("Unable to push metrics: %s", err.Error())
	}

	f.isSending = false
}

func (f *AnalyticsForwarder) HandleUplink(data []byte, addr *net.UDPAddr) {
	log.Debugf("Handling uplink frame from %s: %s", addr.IP.String(), hex.EncodeToString(data))
	frame, err := DecodeMessage(data, len(data), addr, time.Now(), []string{})
	if err != nil {
		log.Warnf("Could not handle uplink: %s", err.Error())
	} else {
		f.uplinkFrame.GatewayEui = frame.GatewayEUI()

		// Convert uplinks
		rx, err := frame.GetAllRxPkt()
		if err == nil && rx != nil {
			for _, r := range rx {
				pkt := f.convertRxPkt(&r)
				log.Debugf("Got uplink: %+v", pkt)
				f.uplinkFrame.Uplinks = append(f.uplinkFrame.Uplinks, pkt)
			}
		}

		// Convert stat
		stat, err := frame.GetStatMsg()
		if err == nil && stat != nil {
			pkt := f.convertStatPkt(stat)
			log.Debugf("Got stat: %+v", pkt)
			f.uplinkFrame.Stats = append(f.uplinkFrame.Stats, pkt)
		}
	}

	log.Debugf("Queue size=%d", f.queueSize())
}

func (f *AnalyticsForwarder) HandleDownlink(data []byte, addr *net.UDPAddr) {
	log.Debugf("Handling downlink frame from %s: %s", addr.IP.String(), hex.EncodeToString(data))
	frame, err := DecodeMessage(data, len(data), addr, time.Now(), []string{})
	if err != nil {
		log.Warnf("Could not handle downlink: %s", err.Error())
	} else {
		f.uplinkFrame.GatewayEui = frame.GatewayEUI()

		// Convert downlinks
		tx, err := frame.GetTxPacket()
		if err == nil && tx != nil {
			log.Debugf("Got downlink: %+v", tx)
			f.uplinkFrame.Downlinks = append(f.uplinkFrame.Downlinks, f.convertTxPkt(tx))
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
		lora.Bandwidth = parseBW(dataRate[2:bw])
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
