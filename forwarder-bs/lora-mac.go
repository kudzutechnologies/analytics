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
	fopts, err := hex.DecodeString(m.FOpts)
	if err != nil || len(fopts) > 15 || int(m.FCtrl&15) != len(fopts) || m.FPort < -1 || m.FPort > 255 {
		return nil, nil, fmt.Errorf("invalid uplink header")
	}
	if m.FPort == -1 && m.FRMPayload != "" {
		return nil, nil, fmt.Errorf("payload without FPort")
	}
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
	if len(fullPayload) > 255 {
		return nil, nil, fmt.Errorf("PHYPayload exceeds 255 bytes")
	}
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
	for i := len(joinEui) - 1; i >= 0; i-- {
		fhdr = append(fhdr, joinEui[i])
	}
	for i := len(devEui) - 1; i >= 0; i-- {
		fhdr = append(fhdr, devEui[i])
	}
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
	if len(fullPayload) == 0 || len(fullPayload) > 255 {
		return nil, nil, fmt.Errorf("invalid proprietary payload size")
	}
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
	n := len(data)
	if n == 0 {
		return 0
	}
	switch (data[0] & 0xE0) >> 5 {
	case MTypeJoinReq:
		if n < 23 {
			return 0
		}
		return 19
	case MTypeRejoinReq:
		if n < 2 {
			return 0
		}
		if data[1] == 1 {
			if n < 24 {
				return 0
			}
			return 20
		}
		if data[1] == 0 || data[1] == 2 {
			if n < 19 {
				return 0
			}
			return 15
		}
		return 0
	case MTypeJoinAccept:
		if n != 17 && n != 33 {
			return 0
		}
		return n
	case MTypeProprietary:
		return n
	default:
		if n < 12 {
			return 0
		}
		opts := int(data[5] & 15)
		if n < 12+opts {
			return 0
		}
		header := 8 + opts
		if n > 12+opts {
			header++
		}
		return header
	}
}

// DecodeDnmsgPdu decodes the pdu hex string from dnmsg and returns raw bytes, fhdr slice, and size.
func DecodeDnmsgPdu(pduHex string) (raw []byte, fhdr []byte, size int, err error) {
	raw, err = hex.DecodeString(pduHex)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(raw) == 0 || len(raw) > 255 {
		return nil, nil, 0, fmt.Errorf("invalid PHYPayload size")
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
