package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
)

type pairConfig struct {
	GatewayID string                 `json:"gateway"`
	ClientID  string                 `json:"client-id"`
	ClientKey string                 `json:"client-key"`
	Extras    map[string]interface{} `json:"extras,omitempty"`
}

// Retrieves the client configuration from a pairing pin
func getClientConfigFromPin(pin string, baseUrl string) (*pairConfig, error) {
	// Make the HTTP GET request to the URL.
	resp, err := http.Get(fmt.Sprintf("%s/%s", baseUrl, pin))
	if err != nil {
		return nil, fmt.Errorf("error making GET request: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	// Parse the JSON data into the Response struct.
	var responseData struct {
		ErrorCode *string `json:"error,omitempty"`
		Details   *struct {
			Description string `json:"description,omitempty"`
		} `json:"details,omitempty"`

		Data *struct {
			Config pairConfig `json:"config"`
		} `json:"data,omitempty"`
	}
	if err := json.Unmarshal(body, &responseData); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	if responseData.ErrorCode != nil {
		if responseData.Details != nil {
			return nil, fmt.Errorf("%s", responseData.Details.Description)
		}
		return nil, fmt.Errorf("%s", *responseData.ErrorCode)
	} else if responseData.Data == nil {
		return nil, fmt.Errorf("error parsing JSON: no data")
	}

	return &responseData.Data.Config, nil
}

func toFlatMap(input interface{}) map[string]interface{} {
	// Convert config to a flat map
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
	baseUrl := "https://eu1.cluster.kudzu.gr/api/v1/pairing/edge"
	if config.Endpoint != "" {
		baseUrl = fmt.Sprintf("https://%s/api/v1/pairing/edge", config.Endpoint)
	}

	// Remove whitespaces and everything non-numeric
	pin = strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, pin)

	pairConfig, err := getClientConfigFromPin(pin, baseUrl)
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
