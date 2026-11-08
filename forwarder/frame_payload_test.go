package main

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestForwarderPreservesFullPayload(t *testing.T) {
	f := &AnalyticsForwarder{}
	up := fromConst(t, PacketPushDataUpContents, SemtechUDPRxPkt{})
	result := f.convertRxPkt(up)
	expected, err := base64.StdEncoding.DecodeString(up.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, result.PhyPayload) {
		t.Fatal("uplink ciphertext or MIC was discarded")
	}
	if !bytes.Equal(result.Fhdr, expected[:len(result.Fhdr)]) || len(result.Fhdr) >= len(expected) {
		t.Fatal("legacy uplink header changed")
	}
	down := fromConst(t, PacketPullRespContents, SemtechUDPTxPkt{})
	d := f.convertTxPkt(down)
	expected, err = base64.StdEncoding.DecodeString(down.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, d.PhyPayload) {
		t.Fatal("downlink ciphertext or MIC was discarded")
	}
	if !bytes.Equal(d.Fhdr, expected[:len(d.Fhdr)]) {
		t.Fatal("legacy downlink header changed")
	}
}
