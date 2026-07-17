package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kudzutechnologies/analytics/client"
)

func renderPairConfig(config ForwarderConfig, pairConfig *client.PairingConfig) string {
	config.ClientId = pairConfig.ClientID
	config.ClientKey = pairConfig.ClientKey
	config.GatewayId = pairConfig.GatewayID

	configMap := toFlatMap(config)
	for k, v := range pairConfig.Extras {
		configMap[k] = v
	}

	defaultConfig := toFlatMap(defaultConf)
	iniConfig := ""
	for k, v := range configMap {
		if dv, ok := defaultConfig[k]; ok && dv == v {
			continue
		}
		iniConfig += fmt.Sprintf("%s=%v\n", k, v)
	}
	return iniConfig
}

func TestRenderPairConfigIncludesCredentialsAndExtras(t *testing.T) {
	config := defaultConf
	config.Endpoint = "analytics.example.com:50051"
	config.CAFile = "/tmp/ca.pem"

	ini := renderPairConfig(config, &client.PairingConfig{
		GatewayID: "aabbccddeeff0011",
		ClientID:  "1122334455667788",
		ClientKey: "deadbeef",
		Extras: map[string]interface{}{
			"log-level": "debug",
		},
	})

	for _, want := range []string{
		"client-id=1122334455667788",
		"client-key=deadbeef",
		"gateway=aabbccddeeff0011",
		"analytics-endpoint=analytics.example.com:50051",
		"analytics-ca-file=/tmp/ca.pem",
		"log-level=debug",
	} {
		if !strings.Contains(ini, want) {
			t.Fatalf("rendered INI missing %q:\n%s", want, ini)
		}
	}
}
