package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/kudzutechnologies/analytics/api"
)

func TestJoinOverAirOrderAndSharedGoldenCorpus(t *testing.T) {
	_, raw, e := ReconstructJreqPayload(&MsgJreq{JoinEui: "0102030405060708", DevEui: "1122334455667788", DevNonce: 0x1234, MIC: 0x12345678})
	if e != nil {
		t.Fatal(e)
	}
	want, _ := hex.DecodeString("0008070605040302018877665544332211341278563412")
	if !bytes.Equal(raw, want) {
		t.Fatalf("join byte order=%x", raw)
	}
	var cases []struct {
		Name, Payload string
		HeaderLength  int    `json:"header_length"`
		UniqueID      string `json:"unique_id"`
	}
	data, e := os.ReadFile("../testdata/basicstation-radio.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(data, &cases); e != nil {
		t.Fatal(e)
	}
	for _, c := range cases {
		payload, _ := hex.DecodeString(c.Payload)
		if n := GetLoRaWANHeaderLen(payload); n != c.HeaderLength {
			t.Errorf("%s header=%d want=%d", c.Name, n, c.HeaderLength)
		}
		up := &api.AnalyticsUplink{Frequency: 868.1, CodingRate: api.LoRaCodingRate_CR_4_5, DataRate: &api.AnalyticsUplink_DataRateLoRa{DataRateLoRa: &api.LoRaDataRate{SpreadingFactor: api.LoRaSF_SF7, Bandwidth: api.LoRaBW_BW_125k}}}
		ComputeUniqueIdUpBS(up, payload)
		if hex.EncodeToString(up.UniqueId) != c.UniqueID {
			t.Errorf("%s dedup mismatch", c.Name)
		}
	}
}

func TestDownlinkDataRatePresence(t *testing.T) {
	f := CreateAnalyticsForwarder(defaultConf, nil, nil)
	table := make(DRTable, 16)
	table[0] = DREntry{12, 125, 0}
	table[3] = DREntry{9, 125, 0}
	table[7] = DREntry{0, 0, 0}
	f.SetDRTable("0102030405060708", table)
	for _, tc := range []struct {
		raw  string
		want api.LoRaSF
	}{
		{`{"msgtype":"dnmsg","dC":1,"DR":3,"Freq":869525000,"pdu":"600102030400000001aabbccdd"}`, api.LoRaSF_SF9},
		{`{"msgtype":"dnmsg","dC":2,"RX2DR":3,"RX2Freq":869525000,"pdu":"600102030400000001aabbccdd"}`, api.LoRaSF_SF9},
		{`{"msgtype":"dnmsg","RX1DR":0,"RX2DR":3,"RX1Freq":868100000,"pdu":"600102030400000001aabbccdd"}`, api.LoRaSF_SF12},
	} {
		m := f.frame("0102030405060708")
		f.handleDnmsg("0102030405060708", []byte(tc.raw), m)
		if len(m.Downlinks) != 1 || m.Downlinks[0].GetDataRateLoRa().SpreadingFactor != tc.want {
			t.Fatalf("wrong DR presence: %+v", m.Downlinks)
		}
	}
	up := f.bsToUplink(7, 868100000, nil, table, time.Now())
	if up.GetDataRateFSK() != 50000 {
		t.Fatal("FSK encoded a DR index as bitrate")
	}
}
