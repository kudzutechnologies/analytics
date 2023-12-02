package main

import (
	"encoding/binary"
	"encoding/json"
	"strconv"
	"strings"

	"fmt"
	"net"
	"time"

	log "github.com/sirupsen/logrus"
)

const PROTOCOL_VERSION = 2
const (
	PUSH_DATA = 0x00
	PUSH_ACK  = 0x01
	PULL_DATA = 0x02
	PULL_RESP = 0x03
	PULL_ACK  = 0x04
	TX_ACK    = 0x05
)

type SemtechUDPMessage struct {
	SenderAddress *net.UDPAddr
	Timestamp     time.Time

	Version byte
	Token   uint16
	Kind    byte
	Data    []byte

	customGwEUI   []byte
	customGwID    string
	parsedPayload *SemtechUDPJsonPayload
}

type SemtechUDPJsonPayload struct {
	RxPackets []SemtechUDPRxPkt `json:"rxpk,omitempty"`
	TxPacket  *SemtechUDPTxPkt  `json:"txpk,omitempty"`
	Stats     *SemtechUDPStat   `json:"stat,omitempty"`
}

type SemtechUDPStat struct {
	Time string  `json:"time,omitempty"`
	Lati float32 `json:"lati,omitempty"`
	Long float32 `json:"long,omitempty"`
	Alti float32 `json:"alti,omitempty"`
	RxNb int     `json:"rxnb,omitempty"`
	RxOk int     `json:"rxok,omitempty"`
	RxFw int     `json:"rxfw,omitempty"`
	Ackr float32 `json:"ackr,omitempty"`
	DwnB int     `json:"dwnb,omitempty"`
	TxNb int     `json:"txnb,omitempty"`
}

type SemtechUDPTxPkt struct {
	Imme       bool    `json:"imme,omitempty"`
	Tmst       int64   `json:"tmst,omitempty"`
	Tmms       int64   `json:"tmms,omitempty"`
	Frequency  float32 `json:"freq,omitempty"`
	RfChain    int     `json:"rfch"`
	Power      float32 `json:"powe,omitempty"`
	Modulation string  `json:"modu,omitempty"`
	DataRate   string  `json:"datr,omitempty"`
	CodingRate string  `json:"codr,omitempty"`
	Fdev       float32 `json:"fdev,omitempty"`
	Ipol       bool    `json:"ipol,omitempty"`
	Prea       int     `json:"prea,omitempty"`
	NoCRC      bool    `json:"ncrc,omitempty"`
	Size       int     `json:"size,omitempty"`
	Data       string  `json:"data,omitempty"`
}

// RSig contains the metadata associated with the received signal
// (Reference from https://github.com/TheThingsArchive/gateway-connector-bridge/blob/master/backend/pktfwd/structs.go)
type SemtechUDPRxPktRsig struct {
	Ant    uint8   `json:"ant"`              // Antenna number on which signal has been received
	Chan   uint8   `json:"chan"`             // Concentrator "IF" channel used for RX (unsigned integer)
	RSSIC  int16   `json:"rssic"`            // RSSI in dBm of the channel (signed integer, 1 dB precision)
	RSSIS  *int16  `json:"rssis,omitempty"`  // RSSI in dBm of the signal (signed integer, 1 DB precision) (Optional)
	RSSISD *uint16 `json:"rssisd,omitempty"` // Standard deviation of RSSI during preamble (unsigned integer) (Optional)
	LSNR   float64 `json:"lsnr"`             // Lora SNR ratio in dB (signed float, 0.1 dB precision)
	ETime  []byte  `json:"etime,omitempty"`  // Encrypted timestamp, ns precision [0..999999999] (Optional)
	FTime  *int64  `json:"ftime,omitempty"`  // Fine timestamp, ns precision [0..999999999] (Optional)
	FOff   *int32  `json:"foff,omitempty"`   // Frequency offset in Hz [-125kHz..+125Khz] (Optional)
}

type SemtechUDPRxPkt struct {
	Time       string  `json:"time,omitempty"`
	Tmms       int64   `json:"tmms,omitempty"`
	Tmst       int64   `json:"tmst,omitempty"`
	Frequency  float32 `json:"freq,omitempty"`
	Channel    int     `json:"chan,omitempty"`
	RfChain    int     `json:"rfch,omitempty"`
	Stat       int     `json:"stat,omitempty"`
	Modulation string  `json:"modu,omitempty"`
	DataRate   string  `json:"datr,omitempty"`
	CodingRate string  `json:"codr,omitempty"`
	Rssi       float32 `json:"rssi,omitempty"`
	Lsnr       float32 `json:"lsnr,omitempty"`
	Size       int     `json:"size,omitempty"`
	Data       string  `json:"data,omitempty"`

	// Extra fields from kerlink, for per-antenna details
	RSig []SemtechUDPRxPktRsig `json:"rsig,omitempty"`

	// Additional Meta-Data exposed from more elaborate
	// internal modules
	DeviceEUI []byte   `json:"-"`
	LocLat    *float32 `json:"-"`
	LocLon    *float32 `json:"-"`
	LocAlt    *float32 `json:"-"`
}

func DecodeMessage(payload []byte, size int, sender *net.UDPAddr, time time.Time, tags []string) (*SemtechUDPMessage, error) {
	if size < 4 {
		return nil, fmt.Errorf("Packet too small")
	}
	if payload[0] != PROTOCOL_VERSION {
		return nil, fmt.Errorf("Invalid protocol version (%d)", payload[0])
	}

	msg := &SemtechUDPMessage{
		SenderAddress: sender,
		Timestamp:     time,
		Version:       payload[0],
		Token:         binary.LittleEndian.Uint16(payload[1:3]),
		Kind:          payload[3],
		Data:          payload[4:size],
		parsedPayload: nil,
	}

	return msg, nil
}

func (m *SemtechUDPMessage) parseJsonPayload() (*SemtechUDPJsonPayload, error) {
	var ret SemtechUDPJsonPayload
	if m.parsedPayload != nil {
		return m.parsedPayload, nil
	}

	var ofs int = 8
	if m.Kind == PULL_RESP {
		ofs = 0
	}

	err := json.Unmarshal(m.Data[ofs:], &ret)
	if err != nil {
		return nil, fmt.Errorf("Could not parse JSON: %s", err.Error())
	}

	return &ret, nil
}

func (m *SemtechUDPMessage) SetCustomGatewayEUI(gwid []byte) {
	m.customGwEUI = gwid
}

func (m *SemtechUDPMessage) SetCustomGatewayID(id string) {
	m.customGwID = id
}

func (m *SemtechUDPMessage) GatewayEUI() []byte {
	if m.customGwEUI != nil {
		return m.customGwEUI
	}
	if m.Kind == PUSH_DATA || m.Kind == PULL_DATA || m.Kind == TX_ACK {
		return m.Data[0:8]
	}
	return nil
}

func (m *SemtechUDPMessage) GatewayID() string {
	if m.customGwID != "" {
		return m.customGwID
	}

	gwIdB := m.GatewayEUI()
	return fmt.Sprintf("eui-%02x%02x%02x%02x%02x%02x%02x%02x",
		gwIdB[0], gwIdB[1], gwIdB[2], gwIdB[3], gwIdB[4], gwIdB[5], gwIdB[6], gwIdB[7])
}

func (m *SemtechUDPMessage) GetBody() []byte {
	if m.Kind == PUSH_DATA {
		return m.Data[8:]
	}
	if m.Kind == PULL_RESP {
		return m.Data
	}
	if m.Kind == TX_ACK {
		return m.Data[8:]
	}

	return nil
}

func (m *SemtechUDPMessage) GetStatMsg() (*SemtechUDPStat, error) {
	if m.Kind == PUSH_DATA {
		ret, err := m.parseJsonPayload()
		if err != nil {
			return nil, err
		}

		return ret.Stats, nil
	}

	return nil, fmt.Errorf("Invalid packet type")
}

func (m *SemtechUDPMessage) GetAllRxPkt() ([]SemtechUDPRxPkt, error) {
	if m.Kind == PUSH_DATA {
		ret, err := m.parseJsonPayload()
		if err != nil {
			return nil, err
		}

		return ret.RxPackets, nil
	}

	return nil, fmt.Errorf("Invalid packet type")
}

func (m *SemtechUDPMessage) GetTxPacket() (*SemtechUDPTxPkt, error) {
	if m.Kind == PULL_RESP {
		ret, err := m.parseJsonPayload()
		if err != nil {
			return nil, err
		}

		return ret.TxPacket, nil
	}

	return nil, fmt.Errorf("Invalid packet type")
}

func (m *SemtechUDPMessage) Encode() []byte {
	totalLen := len(m.Data) + 4
	bytes := make([]byte, totalLen, totalLen)

	bytes[0] = m.Version
	binary.LittleEndian.PutUint16(bytes[1:], m.Token)
	bytes[3] = m.Kind
	copy(bytes[4:], m.Data)

	return bytes
}

func (m *SemtechUDPMessage) Length() int {
	return len(m.Data) + 4
}

////////////////////////////////////////////////////////////////////////////////////
// High-level structure parsing
////////////////////////////////////////////////////////////////////////////////////

type CodingRate struct {
	LoRaSF  int
	LoRaBw  int
	FskRate int
}

func (pkt *SemtechUDPTxPkt) GetCodingRate() (CodingRate, error) {
	var codr CodingRate

	if pkt.Modulation == "LORA" {
		bw := strings.Index(pkt.DataRate, "BW")
		if bw == -1 {
			log.Warnf("Unparsable data rate '%s'", pkt.DataRate)
		} else {
			val, err := strconv.Atoi(pkt.DataRate[2:bw])
			if err == nil {
				codr.LoRaSF = val
			}
			val, err = strconv.Atoi(pkt.DataRate[bw+2:])
			if err == nil {
				codr.LoRaBw = val
			}
		}
	} else if pkt.Modulation == "FSK" {
		val, err := strconv.Atoi(pkt.DataRate)
		if err != nil {
			codr.FskRate = val
		}
	} else {
		log.Warnf("Unknown modulation '%s'", pkt.Modulation)
	}

	return codr, nil
}

func (pkt *SemtechUDPRxPkt) GetCodingRate() (CodingRate, error) {
	var codr CodingRate

	if pkt.Modulation == "LORA" {
		bw := strings.Index(pkt.DataRate, "BW")
		if bw == -1 {
			log.Warnf("Unparsable data rate '%s'", pkt.DataRate)
		} else {
			val, err := strconv.Atoi(pkt.DataRate[2:bw])
			if err == nil {
				codr.LoRaSF = val
			}
			val, err = strconv.Atoi(pkt.DataRate[bw+2:])
			if err == nil {
				codr.LoRaBw = val
			}
		}
	} else if pkt.Modulation == "FSK" {
		val, err := strconv.Atoi(pkt.DataRate)
		if err != nil {
			codr.FskRate = val
		}
	} else {
		log.Warnf("Unknown modulation '%s'", pkt.Modulation)
	}

	return codr, nil
}

////////////////////////////////////////////////////////////////////////////////////
// Helpers
////////////////////////////////////////////////////////////////////////////////////

func SemtechUDPIsDownlink(b []byte) bool {
	if b[0] == PROTOCOL_VERSION {
		switch b[3] {
		case PUSH_DATA:
			return false
		case PUSH_ACK:
			return false

		case PULL_DATA:
			return true
		case PULL_RESP:
			return true
		case PULL_ACK:
			return true
		case TX_ACK:
			return true
		}
	}

	return false
}

func SemtechUDPIsUplink(b []byte) bool {
	if b[0] == PROTOCOL_VERSION {
		switch b[3] {
		case PUSH_DATA:
			return true
		case PUSH_ACK:
			return true

		case PULL_DATA:
			return false
		case PULL_RESP:
			return false
		case PULL_ACK:
			return false
		case TX_ACK:
			return false
		}
	}

	return false
}
