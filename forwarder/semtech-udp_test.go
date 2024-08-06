package main

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	// Raw data from kerlink gateways
	PacketPullReq = "Ar4XAnB2/wBWBgPl"
	PacketPullAck = "Ar4XBA=="

	PacketPushDataStat         = "Ar42AHB2/wBWBgPleyJzdGF0Ijp7ImFja3IiOjEwMC4wLCJib290IjoiMjAyMy0wMi0yMiAwMTowNTowNiBHTVQiLCJkd25iIjowLCJmcGdhIjozMSwiaGFsIjoiNS4wLjEiLCJscHBzIjozMCwicGluZyI6MTIwLCJyeGZ3IjoxLCJyeG5iIjoxLCJyeG9rIjowLCJ0aW1lIjoiMjAyMy0wMi0yMiAwMTo1MzowNyBHTVQiLCJ0eG5iIjowfX0="
	PacketPushDataStatContents = `{
		"time": "2023-02-22 01:53:07 GMT", 
		"rxnb": 1, 
		"rxfw": 1, 
		"ackr": 100 
	}`

	PacketPushDataUp         = "Ar43AHB2/wBWBgPleyJyeHBrIjpbeyJhZXNrIjowLCJicmQiOjAsImNvZHIiOiI0LzUiLCJkYXRhIjoiUUt5ZEN5WUFRd01CN2l1NVFENnNINXUxQytZaCIsImRhdHIiOiJTRjlCVzEyNSIsImZyZXEiOjg2Ny4xLCJqdmVyIjoyLCJtb2R1IjoiTE9SQSIsInJzaWciOlt7ImFudCI6MCwiY2hhbiI6MCwibHNuciI6MTMuMiwicnNzaWMiOi01MH1dLCJzaXplIjoyMSwic3RhdCI6MSwidGltZSI6IjIwMjMtMDItMjJUMDE6NTM6MzEuMzA2MjI0WiIsInRtc3QiOjM4MDA1OTUyODR9XX0="
	PacketPushDataUpContents = `{
		"time": "2023-02-22T01:53:31.306224Z",
		"tmst": 3800595284,
		"freq": 867.1,
		"stat": 1,
		"modu": "LORA",
		"datr": "SF9BW125",
		"codr": "4/5",
		"size": 21,
		"data": "QKydCyYAQwMB7iu5QD6sH5u1C+Yh",
		"rsig": [{ "ant": 0, "chan": 0, "rssic": -50, "lsnr": 13.2 }]
	}`

	PacketPushAck = "Ar43AQ=="

	PacketPullResp         = "AgAEA3sidHhwayI6eyJpbW1lIjpmYWxzZSwidG1zdCI6NDI1NDM3MDM5NiwiZnJlcSI6ODY4LjMsInJmY2giOjAsInBvd2UiOjE0LCJtb2R1IjoiTE9SQSIsImRhdHIiOiJTRjdCVzEyNSIsImNvZHIiOiI0LzUiLCJpcG9sIjp0cnVlLCJzaXplIjozMywibmNyYyI6dHJ1ZSwiZGF0YSI6IklHK1NCcGU1TlVvNEk4TDNpQ1RzbUlnWFBFSERMNjNFcWo2bGFWbXJHS1JGIn19"
	PacketPullRespContents = `{
			"tmst": 4254370396,
			"freq": 868.3,
			"rfch": 0,
			"powe": 14,
			"modu": "LORA",
			"datr": "SF7BW125",
			"codr": "4/5",
			"ipol": true,
			"ncrc": true,
			"size": 33,
			"data": "IG+SBpe5NUo4I8L3iCTsmIgXPEHDL63Eqj6laVmrGKRF"
		}`

	PacketTxAck = "AgAEBXB2/wBWBgPleyJ0eHBrX2FjayI6eyJlcnJvciI6Ik5PTkUifX0="
)

func fromConst[T any](t *testing.T, input string, inst T) *T {
	var copy T = inst
	err := json.Unmarshal([]byte(input), &copy)
	if err != nil {
		t.Errorf("Could not decode constant JSON: %s", err.Error())
		t.Fail()
	}

	return &copy
}

func decodeConstPayload(t *testing.T, str string) *SemtechUDPMessage {
	d, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		t.Errorf("Could not decode constant: %s", err.Error())
		t.Fail()
		return nil
	}

	someAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:123")
	if err != nil {
		t.Errorf("Could not parse sample addr: %s", err.Error())
		t.Fail()
		return nil
	}

	ret, err := DecodeMessage(d, len(d), someAddr, time.Now(), []string{})
	if err != nil {
		t.Errorf("Could not decode message: %s", err.Error())
		t.Fail()
		return nil
	}

	return ret
}

func TestParser(t *testing.T) {
	// [Gateway] Pull data request
	p1 := decodeConstPayload(t, PacketPullReq)
	assert.Equal(t, byte(PULL_DATA), p1.Kind, "Not PULL_DATA")
	assert.Equal(t, uint16(0x17be), p1.Token, "Unexpected tag")
	assert.Equal(t, []byte{0x70, 0x76, 0xff, 0x0, 0x56, 0x6, 0x3, 0xe5}, p1.GatewayEUI(), "Unexpected gateway ID")

	// [Server] Acknowledge PULL
	p2 := decodeConstPayload(t, PacketPullAck)
	assert.Equal(t, byte(PULL_ACK), p2.Kind, "Not PULL_ACK")
	assert.Equal(t, uint16(0x17be), p2.Token, "Unexpected tag")
	assert.Nil(t, p2.GatewayEUI())

	// [Gateway] Push only stats
	p3 := decodeConstPayload(t, PacketPushDataStat)
	assert.Equal(t, byte(PUSH_DATA), p3.Kind, "Not PUSH_DATA")
	assert.Equal(t, uint16(0x36be), p3.Token, "Unexpected tag")
	assert.Equal(t, []byte{0x70, 0x76, 0xff, 0x0, 0x56, 0x6, 0x3, 0xe5}, p3.GatewayEUI(), "Unexpected gateway ID")
	js3, err := p3.GetStatMsg()
	assert.NoError(t, err, "GetStatMsg error")
	assert.Equal(t,
		fromConst(t, PacketPushDataStatContents, SemtechUDPStat{}),
		js3, "Unexpected packet contents",
	)
	jd3, err := p3.GetAllRxPkt()
	assert.NoError(t, err, "GetAllRxPkt error")
	assert.Len(t, jd3, 0)

	// [Gateway] Push only data
	p4 := decodeConstPayload(t, PacketPushDataUp)
	assert.Equal(t, byte(PUSH_DATA), p4.Kind, "Not PUSH_DATA")
	assert.Equal(t, uint16(0x37be), p4.Token, "Unexpected tag")
	assert.Equal(t, []byte{0x70, 0x76, 0xff, 0x0, 0x56, 0x6, 0x3, 0xe5}, p4.GatewayEUI(), "Unexpected gateway ID")
	js4, err := p4.GetStatMsg()
	assert.NoError(t, err, "GetStatMsg error")
	assert.Nil(t, js4)
	jd4, err := p4.GetAllRxPkt()
	assert.NoError(t, err, "GetAllRxPkt error")
	assert.Len(t, jd4, 1)
	assert.Equal(t,
		fromConst(t, PacketPushDataUpContents, SemtechUDPRxPkt{}),
		&jd4[0], "Unexpected packet contents",
	)

	// [Server] Acknowledge PUSH
	p5 := decodeConstPayload(t, PacketPushAck)
	assert.Equal(t, byte(PUSH_ACK), p5.Kind, "Not PUSH_ACK")
	assert.Equal(t, uint16(0x37be), p5.Token, "Unexpected tag")
	assert.Nil(t, p5.GatewayEUI())

	// [Server] Pull data
	p6 := decodeConstPayload(t, PacketPullResp)
	assert.Equal(t, byte(PULL_RESP), p6.Kind, "Not PULL_RESP")
	assert.Equal(t, uint16(0x0400), p6.Token, "Unexpected tag")
	assert.Nil(t, p6.GatewayEUI())
	jt5, err := p6.GetTxPacket()
	assert.NoError(t, err, "GetTxPacket error")
	assert.Equal(t,
		fromConst(t, PacketPullRespContents, SemtechUDPTxPkt{}),
		jt5, "Unexpected packet contents",
	)

	// [Gateway] Acknowledge transmission
	p7 := decodeConstPayload(t, PacketTxAck)
	assert.Equal(t, byte(TX_ACK), p7.Kind, "Not TX_ACK")
	assert.Equal(t, uint16(0x0400), p7.Token, "Unexpected tag")
	assert.Equal(t, []byte{0x70, 0x76, 0xff, 0x0, 0x56, 0x6, 0x3, 0xe5}, p7.GatewayEUI(), "Unexpected gateway ID")
}
