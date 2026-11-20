package main

import "testing"

func FuzzCUPSResponse(f *testing.F) {
	f.Add(make([]byte, CupsNullResponseLen))
	f.Add([]byte{0x30, 0x80})
	f.Add([]byte{0x30, 0x84, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 65536 {
			return
		}
		_, _ = ParseCUPSUpdateResponseFull(raw)
		_, _ = derObjectLength(raw)
		_, _, _, _, _ = decodeCredential(raw)
	})
}
