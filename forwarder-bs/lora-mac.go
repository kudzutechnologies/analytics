package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/kudzutechnologies/analytics/api"
)

const (
	MTypeJoinReq         = 0x00
	MTypeJoinAccept      = 0x01
	MTypeUnconfirmedUp   = 0x02
	MTypeUnconfirmedDown = 0x03
	MTypeConfirmedUp     = 0x04
	MTypeConfirmedDown   = 0x05
	MTypeRejoinReq       = 0x06
	MTypeProprietary     = 0x07
)

// euiToBytes parses an EUI string (ID6 or hex with optional separators) into 8 bytes big-endian.
func euiToBytes(eui string) ([]byte, error) {
	eui = strings.ReplaceAll(eui, "-", "")
	eui = strings.ReplaceAll(eui, ":", "")
	if len(eui) != 16 {
		return nil, fmt.Errorf("eui must be 16 hex chars, got %d", len(eui))
	}
	return hex.DecodeString(eui)
}

// ReconstructUpdfPayload reconstructs the full PHYPayload (FHDR + FPort + FRMPayload + MIC) from MsgUpdf.
// Returns fhdr (header only), fullPayload (for uniqueId), and any error.
func ReconstructUpdfPayload(m *MsgUpdf) (fhdr []byte, fullPayload []byte, err error) {
	// FHDR: MHdr(1) + DevAddr(4 LE) + FCtrl(1) + FCnt(2 LE) + FOpts
	fopts, _ := hex.DecodeString(m.FOpts)
	fhdrLen := 1 + 4 + 1 + 2 + len(fopts)
	if m.FPort >= 0 {
		fhdrLen++ // FPort byte
	}
	fhdr = make([]byte, 0, fhdrLen)
	fhdr = append(fhdr, m.MHdr)
	devAddr := make([]byte, 4)
	binary.LittleEndian.PutUint32(devAddr, uint32(m.DevAddr))
	fhdr = append(fhdr, devAddr...)
	fhdr = append(fhdr, m.FCtrl)
	fcnt := make([]byte, 2)
	binary.LittleEndian.PutUint16(fcnt, m.FCnt)
	fhdr = append(fhdr, fcnt...)
	fhdr = append(fhdr, fopts...)
	if m.FPort >= 0 {
		fhdr = append(fhdr, byte(m.FPort))
	}

	frm, err := hex.DecodeString(m.FRMPayload)
	if err != nil {
		return fhdr, nil, err
	}
	mic := make([]byte, 4)
	binary.LittleEndian.PutUint32(mic, uint32(m.MIC))
	fullPayload = append(append(append([]byte{}, fhdr...), frm...), mic...)
	return fhdr, fullPayload, nil
}

// ReconstructJreqPayload reconstructs the join request PHYPayload: MHdr + JoinEui(8) + DevEui(8) + DevNonce(2 LE) + MIC(4).
func ReconstructJreqPayload(m *MsgJreq) (fhdr []byte, fullPayload []byte, err error) {
	joinEui, err := euiToBytes(m.JoinEui)
	if err != nil {
		return nil, nil, err
	}
	devEui, err := euiToBytes(m.DevEui)
	if err != nil {
		return nil, nil, err
	}
	// Join request: MHdr(1) + JoinEui(8) + DevEui(8) + DevNonce(2 LE) + MIC(4) = 23 bytes
	// "Header" for analytics we take as first 19 bytes (up to and including DevNonce); MIC is not part of FHDR in same way.
	fhdr = make([]byte, 0, 19)
	fhdr = append(fhdr, m.MHdr)
	fhdr = append(fhdr, joinEui...)
	fhdr = append(fhdr, devEui...)
	devNonce := make([]byte, 2)
	binary.LittleEndian.PutUint16(devNonce, m.DevNonce)
	fhdr = append(fhdr, devNonce...)
	mic := make([]byte, 4)
	binary.LittleEndian.PutUint32(mic, uint32(m.MIC))
	fullPayload = append(append([]byte{}, fhdr...), mic...)
	return fhdr, fullPayload, nil
}

// ReconstructPropdfPayload decodes FRMPayload (entire PHYPayload) and returns header length and full payload.
func ReconstructPropdfPayload(m *MsgPropdf) (fhdr []byte, fullPayload []byte, err error) {
	fullPayload, err = hex.DecodeString(m.FRMPayload)
	if err != nil {
		return nil, nil, err
	}
	fhdrLen := GetLoRaWANHeaderLen(fullPayload)
	if fhdrLen > len(fullPayload) {
		fhdrLen = len(fullPayload)
	}
	fhdr = fullPayload[:fhdrLen]
	return fhdr, fullPayload, nil
}

// GetLoRaWANHeaderLen returns the length of the MAC header in a raw PHYPayload.
func GetLoRaWANHeaderLen(data []byte) int {
	dlen := len(data)
	if dlen < 1 {
		return 0
	}
	mhdr := data[0]
	mtype := (mhdr & 0xE0) >> 5

	if mtype == MTypeJoinReq || mtype == MTypeRejoinReq {
		if dlen < 19 {
			return dlen
		}
		return 19
	}
	if mtype == MTypeJoinAccept {
		return dlen
	}
	if mtype == MTypeProprietary {
		return dlen
	}
	if dlen < 8 {
		return dlen
	}
	fctrl := data[5]
	foptsLen := int(fctrl & 0x0F)
	if dlen < 8+foptsLen+1 {
		return dlen
	}
	return 8 + foptsLen + 1
}

// DecodeDnmsgPdu decodes the pdu hex string from dnmsg and returns raw bytes, fhdr slice, and size.
func DecodeDnmsgPdu(pduHex string) (raw []byte, fhdr []byte, size int, err error) {
	raw, err = hex.DecodeString(pduHex)
	if err != nil {
		return nil, nil, 0, err
	}
	size = len(raw)
	fhdrLen := GetLoRaWANHeaderLen(raw)
	if fhdrLen > len(raw) {
		fhdrLen = len(raw)
	}
	fhdr = raw[:fhdrLen]
	return raw, fhdr, size, nil
}

// ComputeUniqueIdUpBS is a wrapper that calls api.ComputeUniqueIdUp after setting fhdr and full payload.
func ComputeUniqueIdUpBS(up *api.AnalyticsUplink, fullPayload []byte) {
	api.ComputeUniqueIdUp(up, fullPayload)
}

// ComputeUniqueIdDownBS is a wrapper that calls api.ComputeUniqueIdDown.
func ComputeUniqueIdDownBS(down *api.AnalyticsDownlink, fullPayload []byte) {
	api.ComputeUniqueIdDown(down, fullPayload)
}
