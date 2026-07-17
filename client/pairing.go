package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

const (
	// DefaultPairingBaseURL is the default HTTPS endpoint used for edge pairing.
	DefaultPairingBaseURL = "https://console.eu1.cluster.kudzu.gr/api/v1/pairing/edge"
	defaultPairingTimeout = 30 * time.Second
)

// PairingOptions configures a pairing request.
type PairingOptions struct {
	// Pin is the pairing PIN. Non-digit characters are stripped before use.
	Pin string
	// BaseURL is the pairing API base URL without the trailing PIN segment.
	// When empty, DefaultPairingBaseURL is used.
	BaseURL string
	// Endpoint is an optional analytics host[:port]. When set and BaseURL is
	// empty, the pairing URL becomes https://{Endpoint}/api/v1/pairing/edge.
	Endpoint string
	// CAFile is an optional PEM CA bundle appended to the system trust store.
	CAFile string
	// Timeout overrides the HTTP request timeout. Zero uses the default.
	Timeout time.Duration
}

// PairingConfig is the configuration returned by a successful pairing request.
type PairingConfig struct {
	GatewayID string                 `json:"gateway"`
	ClientID  string                 `json:"client-id"`
	ClientKey string                 `json:"client-key"`
	Extras    map[string]interface{} `json:"extras,omitempty"`
}

// NormalizePairingPin keeps only ASCII digits from pin.
func NormalizePairingPin(pin string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, pin)
}

// PairingBaseURL resolves the pairing base URL from options.
func PairingBaseURL(opts PairingOptions) string {
	if opts.BaseURL != "" {
		return strings.TrimRight(opts.BaseURL, "/")
	}
	if opts.Endpoint != "" {
		return fmt.Sprintf("https://%s/api/v1/pairing/edge", opts.Endpoint)
	}
	return DefaultPairingBaseURL
}

// FetchPairingConfig downloads edge pairing credentials over HTTPS using the
// same additive TLS trust rules as the analytics gRPC client.
func FetchPairingConfig(opts PairingOptions) (*PairingConfig, error) {
	pin := NormalizePairingPin(opts.Pin)
	if pin == "" {
		return nil, fmt.Errorf("pairing PIN must contain digits")
	}

	tlsConfig, err := loadTLSConfig(opts.CAFile, nil)
	if err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultPairingTimeout
	}

	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	url := fmt.Sprintf("%s/%s", PairingBaseURL(opts), pin)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("error making GET request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pairing request failed with status %s", resp.Status)
	}

	var responseData struct {
		ErrorCode *string `json:"error,omitempty"`
		Details   *struct {
			Description string `json:"description,omitempty"`
		} `json:"details,omitempty"`
		Data *struct {
			Config PairingConfig `json:"config"`
		} `json:"data,omitempty"`
	}
	if err := json.Unmarshal(body, &responseData); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	if responseData.ErrorCode != nil {
		if responseData.Details != nil && responseData.Details.Description != "" {
			return nil, fmt.Errorf("%s", responseData.Details.Description)
		}
		return nil, fmt.Errorf("%s", *responseData.ErrorCode)
	}
	if responseData.Data == nil {
		return nil, fmt.Errorf("error parsing JSON: no data")
	}

	return &responseData.Data.Config, nil
}
