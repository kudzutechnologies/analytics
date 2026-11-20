package main

import "testing"

func TestSemtechTimestampUnits(t *testing.T) {
	for _, tc := range []struct {
		name, input    string
		micros, millis int64
	}{
		{"utc_fraction", "2023-11-14T22:13:20.123456789Z", 1700000000123456, 1700000000123},
		{"offset", "2023-11-15T00:13:20.123456789+02:00", 1700000000123456, 1700000000123},
		{"negative_offset", "2023-11-14T15:13:20.123456789-07:00", 1700000000123456, 1700000000123},
		{"whole_seconds", "2023-11-14T22:13:20Z", 1700000000000000, 1700000000000},
		{"short_fraction", "2023-11-14T22:13:20.1Z", 1700000000100000, 1700000000100},
		{"missing", "", 0, 0},
		{"invalid_date", "2023-02-29T22:13:20Z", 0, 0},
		{"trailing_text", "2023-11-14T22:13:20Zjunk", 0, 0},
		{"invalid_offset", "2023-11-14T22:13:20+AA:00", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forwarder := &AnalyticsForwarder{}
			up := forwarder.convertRxPkt(&SemtechUDPRxPkt{Time: tc.input, Tmst: 4294967290, Tmms: 1384035218123})
			if up.RxWallTime != tc.micros || up.RxFinishedTime != 4294967290 || up.RxGpsTime != 1384035218123 {
				t.Fatalf("unexpected RX clock units: wall=%d counter=%d gps=%d", up.RxWallTime, up.RxFinishedTime, up.RxGpsTime)
			}
			stat := forwarder.convertStatPkt(&SemtechUDPStat{Time: tc.input})
			if stat.GwTime != tc.millis {
				t.Fatalf("stat UTC milliseconds: got %d, want %d", stat.GwTime, tc.millis)
			}
		})
	}
}
