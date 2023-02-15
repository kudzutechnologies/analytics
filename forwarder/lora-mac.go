package main

const (
	MTypeJoinReq         = 0x00
	MTypeJoinAccept      = 0x01
	MTYpeUnconfirmedUp   = 0x02
	MTYpeUnconfirmedDown = 0x03
	MTypeConfirmedUp     = 0x04
	MTypeConfirmedDown   = 0x05
	MTYpeRejoinReq       = 0x06
	MTYpeProprietary     = 0x07
)

func GetLoRaWANHeaderLen(data []byte) int {
	dlen := len(data)
	if dlen < 1 {
		return 0
	}

	// MAC Header
	mhdr := data[0]
	mtype := (mhdr & 0xE0) >> 5

	if mtype == MTypeJoinReq || mtype == MTYpeRejoinReq {
		// OTAA request frames have fixed lengh
		if dlen < 19 {
			return dlen
		}
		return 19
	} else if mtype == MTypeJoinAccept {
		// OTAA response frames have fixed lengh, but they
		// can optionally extend with the variable CFList
		return dlen
	} else if mtype == MTYpeProprietary {
		// We don't know anything for proprietary, so we
		// should collect everything
		return dlen
	} else {
		if dlen < 8 {
			return dlen
		}

		// Otherwise we have a data frame
		fctrl := data[5]
		foptsLen := int(fctrl & 0x0F)
		if len(data) < 8+foptsLen+1 {
			// FOpts too small, probably not a LoaWAN frame
			return len(data)
		}

		// Usually there is a port byte right after, collect it
		return 8 + foptsLen + 1
	}
}
