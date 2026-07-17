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
		Endpoint: config.Endpoint,
		CAFile:   config.CAFile,
	})
	if err != nil {
		return "", err
	}

	// Update known config properties
	config.ClientId = pairConfig.ClientID
	config.ClientKey = pairConfig.ClientKey
	config.GatewayId = pairConfig.GatewayID

	// Merge extras
	configMap := toFlatMap(config)
	for k, v := range pairConfig.Extras {
		configMap[k] = v
	}

	// Render to INI format
	iniConfig := ""
	defaultConfig := toFlatMap(defaultConf)
	for k, v := range configMap {
		if dv, ok := defaultConfig[k]; ok && dv == v {
			continue
		}
		iniConfig += fmt.Sprintf("%s=%v\n", k, v)
	}

	return iniConfig, nil
}
