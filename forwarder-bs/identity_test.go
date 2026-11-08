package main

import (
	"encoding/json"
	"testing"
)

func TestIdentityCanonicalForms(t *testing.T) {
	for input, want := range map[string]string{"f::1": "000f000000000001", "::a:b": "00000000000a000b", "f:a123:f8:100": "000fa12300f80100", "01-02-03-04-05-06-07-08": "0102030405060708", "01:02:03:04:05:06:07:08": "0102030405060708", "FFFFFFFFFFFFFFFF": "ffffffffffffffff"} {
		got, err := ParseGatewayID(input)
		if err != nil || got != want {
			t.Fatalf("%s: %s %v", input, got, err)
		}
		if len(GatewayEUIFromRouter(input)) != 8 {
			t.Fatal("missing analytics EUI")
		}
	}
	for _, input := range []string{"", "../../a", "1::2::3", "1:2:3", "1:2:3:10000", "0x12345678901234"} {
		if _, err := ParseGatewayID(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	var req RouterInfoRequest
	if err := json.Unmarshal([]byte(`{"router":18446744073709551615}`), &req); err != nil || req.Router != "ffffffffffffffff" {
		t.Fatalf("uint64 precision lost: %+v %v", req, err)
	}
	if err := json.Unmarshal([]byte(`{"router":18446744073709551616}`), &req); err == nil {
		t.Fatal("uint64 overflow accepted")
	}
}
