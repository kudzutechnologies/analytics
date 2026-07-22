package main

import (
	"strings"
	"testing"

	"github.com/kudzutechnologies/analytics/client"
)

func TestRenderPairConfigIncludesCredentialsAndExtras(t *testing.T) {
	config := defaultConf
	config.Endpoint = "analytics.example.com:50051"
	config.PairingEndpoint = "https://console.example.com/"
	config.CAFile = "/tmp/ca.pem"

	ini := renderPairConfig(config, &client.PairingConfig{
		GatewayID: "aabbccddeeff0011",
		ClientID:  "1122334455667788",
		ClientKey: "deadbeef",
		Extras: map[string]interface{}{
			"log-level":          "debug",
			"client-id":          "attacker-client",
			"analytics-endpoint": "attacker.example.com:443",
			"pairing-endpoint":   "https://attacker.example.com",
			"analytics-ca-file":  "/tmp/attacker.pem",
		},
	})

	for _, want := range []string{
		"client-id=1122334455667788",
		"client-key=deadbeef",
		"gateway=aabbccddeeff0011",
		"analytics-endpoint=analytics.example.com:50051",
		"pairing-endpoint=https://console.example.com/",
		"analytics-ca-file=/tmp/ca.pem",
		"log-level=debug",
	} {
		if !strings.Contains(ini, want) {
			t.Fatalf("rendered INI missing %q:\n%s", want, ini)
		}
	}
	for _, blocked := range []string{"attacker-client", "attacker.example.com", "attacker.pem"} {
		if strings.Contains(ini, blocked) {
			t.Fatalf("rendered INI contains protected extra %q:\n%s", blocked, ini)
		}
	}
}

func TestDefaultEndpoints(t *testing.T) {
	if defaultConf.Endpoint != "ingress.eu1.cluster.kudzu.gr:443" {
		t.Fatalf("analytics endpoint = %q", defaultConf.Endpoint)
	}
	if defaultConf.PairingEndpoint != "https://console.eu1.cluster.kudzu.gr" {
		t.Fatalf("pairing endpoint = %q", defaultConf.PairingEndpoint)
	}
}
