package main

import (
	"encoding/json"
	"fmt"

	"github.com/kudzutechnologies/analytics/client"
)

func toFlatMap(input interface{}) map[string]interface{} {
	rawConfig := make(map[string]interface{})
	str, err := json.Marshal(input)
	if err != nil {
		panic(fmt.Errorf("internal error while marshalling config: %w", err))
	}
	if err := json.Unmarshal(str, &rawConfig); err != nil {
		panic(fmt.Errorf("internal error while unmarshalling config: %w", err))
	}
	return rawConfig
}

func getRenderedPairConfig(pin string, config ForwarderConfig) (string, error) {
	pairConfig, err := client.FetchPairingConfig(client.PairingOptions{
		Pin:      pin,
		Endpoint: config.PairingEndpoint,
		CAFile:   config.CAFile,
	})
	if err != nil {
		return "", err
	}

	return renderPairConfig(config, pairConfig), nil
}

func renderPairConfig(config ForwarderConfig, pairConfig *client.PairingConfig) string {
	config.ClientId = pairConfig.ClientID
	config.ClientKey = pairConfig.ClientKey
	config.GatewayId = pairConfig.GatewayID

	configMap := toFlatMap(config)
	for k, v := range pairConfig.Extras {
		if client.IsProtectedPairingConfigKey(k) {
			continue
		}
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
