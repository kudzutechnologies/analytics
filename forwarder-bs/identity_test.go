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

func TestDiscoveryMuxsLabelsDoNotUseGatewayIdentityValidation(t *testing.T) {
	for _, tc := range []struct{ muxs, want string }{
		{`"lns-bridge-1"`, "lns-bridge-1"},
		{`"::1"`, "::1"},
		{`""`, ""},
		{`null`, ""},
		{`18446744073709551615`, "ffff:ffff:ffff:ffff"},
	} {
		var response RouterInfoResponse
		raw := `{"router":"24e1:24ff:fef3:7b09","muxs":` + tc.muxs + `,"uri":"wss://lns.example.test/assigned"}`
		if err := json.Unmarshal([]byte(raw), &response); err != nil {
			t.Fatalf("muxs=%s: %v", tc.muxs, err)
		}
		if response.Router != "24e1:24ff:fef3:7b09" || response.Muxs != tc.want {
			t.Fatalf("unexpected discovery response: %+v", response)
		}
	}
	for _, raw := range []string{
		`{"router":"not-a-gateway","muxs":"lns-bridge-1"}`,
		`{"router":18446744073709551616,"muxs":"lns-bridge-1"}`,
		`{"router":"24e124fffef37b09","muxs":{}}`,
	} {
		var response RouterInfoResponse
		if err := json.Unmarshal([]byte(raw), &response); err == nil {
			t.Fatalf("accepted invalid discovery response: %s", raw)
		}
	}
}

func TestDiscoveryRejectionIsNotHiddenByEmptyIdentities(t *testing.T) {
	response := RouterInfoResponse{Router: "old", Muxs: "old", URI: "old"}
	if err := json.Unmarshal([]byte(`{"router":null,"muxs":null,"error":"gateway not authorized"}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "gateway not authorized" || response.Router != "" || response.Muxs != "" || response.URI != "" {
		t.Fatalf("rejection was not preserved: %+v", response)
	}
}
