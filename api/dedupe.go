package api

import (
	"crypto/sha1"
	"encoding/binary"
)

// Data is a pretty good source of entropy, since
// the LoRaWAN frames are encrypted and therefore each
// message is unique.
//
// However this requires the frames to correctly advance
// the frame counter after every transmission. If mote is
// bugys however, this ID will not change, so we need additional
// entropy. The following fields are also the same on every
// transmission:
//
// 1. Frequency
// 2. Modulation, Coding Rate and Data Rate
// 3. Transmission time window

func ComputeUniqueIdUp(up *AnalyticsUplink, fullPayload []byte) {
	var extra []byte
	extra = []byte{0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(extra[0:], uint32(up.Frequency))
	binary.LittleEndian.PutUint16(extra[4:], uint16(up.CodingRate))

	switch dr := up.DataRate.(type) {
	case *AnalyticsUplink_DataRateLoRa:
		extraDr := []byte{1, 0, 0, 0, 0}
		binary.LittleEndian.PutUint16(extraDr[1:], uint16(dr.DataRateLoRa.Bandwidth))
		binary.LittleEndian.PutUint16(extraDr[3:], uint16(dr.DataRateLoRa.SpreadingFactor))
		extra = append(extra, extraDr...)

	case *AnalyticsUplink_DataRateFSK:
		extraDr := []byte{2, 0, 0, 0, 0}
		binary.LittleEndian.PutUint32(extraDr[1:], uint32(dr.DataRateFSK))
		extra = append(extra, extraDr...)
	}

	csum := sha1.Sum(append(fullPayload, extra...))
	up.UniqueId = csum[:]
}

func ComputeUniqueIdDown(up *AnalyticsDownlink, fullPayload []byte) {
	var extra []byte
	extra = []byte{0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(extra[0:], uint32(up.Frequency))
	binary.LittleEndian.PutUint16(extra[4:], uint16(up.CodingRate))

	switch dr := up.DataRate.(type) {
	case *AnalyticsDownlink_DataRateLoRa:
		extraDr := []byte{1, 0, 0, 0, 0}
		binary.LittleEndian.PutUint16(extraDr[1:], uint16(dr.DataRateLoRa.Bandwidth))
		binary.LittleEndian.PutUint16(extraDr[3:], uint16(dr.DataRateLoRa.SpreadingFactor))
		extra = append(extra, extraDr...)

	case *AnalyticsDownlink_DataRateFSK:
		extraDr := []byte{2, 0, 0, 0, 0}
		binary.LittleEndian.PutUint32(extraDr[1:], uint32(dr.DataRateFSK))
		extra = append(extra, extraDr...)
	}

	csum := sha1.Sum(append(fullPayload, extra...))
	up.UniqueId = csum[:]
}
