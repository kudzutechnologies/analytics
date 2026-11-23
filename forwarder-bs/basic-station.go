package forwarderbs

import (
	"encoding/json"
)

// UpInfo is the radio metadata present in all uplink radio messages.
type UpInfo struct {
	Rctx    int64   `json:"rctx"`
	Xtime   int64   `json:"xtime"`
	Gpstime int64   `json:"gpstime"`
	Rssi    float64 `json:"rssi"`
	Snr     float64 `json:"snr"`
}

// DREntry is one data rate entry from router_config: [SF, BW, DNONLY].
// SF 7-12 for LoRa, 0 for FSK. BW 125, 250, or 500 (ignored for FSK).
// DNONLY 1 = downlink only, 0 = uplink and downlink.
type DREntry [3]int

// DRTable is the 16-entry data rate table from router_config.
type DRTable []DREntry

// MsgVersion is the first message sent by the Station after WebSocket connect.
type MsgVersion struct {
	Msgtype  string `json:"msgtype"`
	Station  string `json:"station"`
	Firmware string `json:"firmware"`
	Package  string `json:"package"`
	Model    string `json:"model"`
	Protocol int    `json:"protocol"`
	Features string `json:"features"`
}

// MsgRouterConfig is the LNS response to version; contains channel plan and DRs.
type MsgRouterConfig struct {
	Msgtype    string        `json:"msgtype"`
	NetID      []int         `json:"NetID,omitempty"`
	JoinEui    [][2]int      `json:"JoinEui,omitempty"`
	Region     string        `json:"region,omitempty"`
	MaxEirp    float64       `json:"max_eirp,omitempty"`
	Hwspec     string        `json:"hwspec,omitempty"`
	FreqRange  [2]int        `json:"freq_range,omitempty"`
	DRs        []DREntry     `json:"DRs,omitempty"`
	Sx1301Conf []interface{} `json:"sx1301_conf,omitempty"`
	Bcning     interface{}   `json:"bcning,omitempty"`
	Nocca      bool          `json:"nocca,omitempty"`
	Nodc       bool          `json:"nodc,omitempty"`
	Nodwell    bool          `json:"nodwell,omitempty"`
}

// MsgJreq is a LoRaWAN join request uplink.
type MsgJreq struct {
	Msgtype  string  `json:"msgtype"`
	MHdr     uint8   `json:"MHdr"`
	JoinEui  string  `json:"JoinEui"`
	DevEui   string  `json:"DevEui"`
	DevNonce uint16  `json:"DevNonce"`
	MIC      int32   `json:"MIC"`
	DR       int     `json:"DR"`
	Freq     int64   `json:"Freq"`
	Upinfo   *UpInfo `json:"upinfo,omitempty"`
}

// MsgUpdf is a LoRaWAN data frame uplink.
type MsgUpdf struct {
	Msgtype    string  `json:"msgtype"`
	MHdr       uint8   `json:"MHdr"`
	DevAddr    int32   `json:"DevAddr"`
	FCtrl      uint8   `json:"FCtrl"`
	FCnt       uint16  `json:"FCnt"`
	FOpts      string  `json:"FOpts"`
	FPort      int     `json:"FPort"`
	FRMPayload string  `json:"FRMPayload"`
	MIC        int32   `json:"MIC"`
	DR         int     `json:"DR"`
	Freq       int64   `json:"Freq"`
	Upinfo     *UpInfo `json:"upinfo,omitempty"`
}

// MsgPropdf is a proprietary frame uplink (entire PHYPayload in FRMPayload).
type MsgPropdf struct {
	Msgtype    string  `json:"msgtype"`
	FRMPayload string  `json:"FRMPayload"`
	DR         int     `json:"DR"`
	Freq       int64   `json:"Freq"`
	Upinfo     *UpInfo `json:"upinfo,omitempty"`
}

// MsgDntxed is transmit confirmation from Station to LNS.
type MsgDntxed struct {
	Msgtype string  `json:"msgtype"`
	Diid    int64   `json:"diid"`
	DevEui  string  `json:"DevEui"`
	Rctx    int64   `json:"rctx"`
	Xtime   int64   `json:"xtime"`
	Txtime  float64 `json:"txtime"`
	Gpstime int64   `json:"gpstime"`
}

// MsgDnmsg is a downlink message (Class A, B, or C).
type MsgDnmsg struct {
	Msgtype  string `json:"msgtype"`
	DevEui   string `json:"DevEui"`
	DC       int    `json:"dC"`
	Diid     int64  `json:"diid"`
	Pdu      string `json:"pdu"`
	RxDelay  int    `json:"RxDelay,omitempty"`
	RX1DR    *int   `json:"RX1DR,omitempty"`
	RX1Freq  int64  `json:"RX1Freq,omitempty"`
	RX2DR    *int   `json:"RX2DR,omitempty"`
	RX2Freq  int64  `json:"RX2Freq,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Xtime    int64  `json:"xtime,omitempty"`
	Rctx     int64  `json:"rctx,omitempty"`
	DR       *int   `json:"DR,omitempty"`
	Freq     int64  `json:"Freq,omitempty"`
	Gpstime  int64  `json:"gpstime,omitempty"`
}

// MsgTimesync can be upstream (txtime only) or downstream (gpstime, xtime).
type MsgTimesync struct {
	Msgtype string `json:"msgtype"`
	Txtime  int64  `json:"txtime,omitempty"`
	Xtime   int64  `json:"xtime,omitempty"`
	Gpstime int64  `json:"gpstime,omitempty"`
}

// RouterInfoRequest is sent by Station to /router-info (discovery).
type RouterInfoRequest struct {
	Router string `json:"router"`
}

// RouterInfoResponse is the LNS response to /router-info.
type RouterInfoResponse struct {
	Router string `json:"router,omitempty"`
	Muxs   string `json:"muxs,omitempty"`
	URI    string `json:"uri,omitempty"`
	Error  string `json:"error,omitempty"`
}

// GatewayLNSConfig is the LNS-allocated configuration for a gateway (from router-info response).
// Stored per gateway so the forwarder uses LNS-provided tc.uri and related options instead of config file.
type GatewayLNSConfig struct {
	Router string `json:"router,omitempty" bson:"router,omitempty"`
	Muxs   string `json:"muxs,omitempty" bson:"muxs,omitempty"`
	URI    string `json:"uri,omitempty" bson:"uri,omitempty"`
}

// GetMsgtype returns the msgtype field from a JSON object without full unmarshaling.
func GetMsgtype(raw []byte) (string, error) {
	var m struct {
		Msgtype string `json:"msgtype"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", err
	}
	return m.Msgtype, nil
}

// ParseRouterConfig extracts DRs from a router_config message for use in DR resolution.
func ParseRouterConfig(raw []byte) (*MsgRouterConfig, error) {
	var m struct {
		DRs []DREntry `json:"DRs"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &MsgRouterConfig{DRs: m.DRs}, nil
}

// ParseUplinkMessage parses jreq, updf, or propdf into a generic structure for analytics.
func ParseUplinkMessage(raw []byte) (msgtype string, err error) {
	msgtype, err = GetMsgtype(raw)
	if err != nil {
		return "", err
	}
	return msgtype, nil
}

// ParseJreq parses a jreq message.
func ParseJreq(raw []byte) (*MsgJreq, error) {
	var m MsgJreq
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ParseUpdf parses an updf message.
func ParseUpdf(raw []byte) (*MsgUpdf, error) {
	var m MsgUpdf
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ParsePropdf parses a propdf message.
func ParsePropdf(raw []byte) (*MsgPropdf, error) {
	var m MsgPropdf
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ParseDnmsg parses a dnmsg message.
func ParseDnmsg(raw []byte) (*MsgDnmsg, error) {
	var m MsgDnmsg
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ParseDntxed parses a dntxed message.
func ParseDntxed(raw []byte) (*MsgDntxed, error) {
	var m MsgDntxed
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// GetDRTable returns the DR table from router_config (exactly 16 entries).
func (c *MsgRouterConfig) GetDRTable() DRTable {
	if len(c.DRs) != 16 {
		return nil
	}
	return c.DRs[:16]
}
