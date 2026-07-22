package client

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizePairingPin(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"123-456", "123456"},
		{" 12 34 ", "1234"},
		{"abc", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizePairingPin(tt.in); got != tt.want {
			t.Errorf("NormalizePairingPin(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPairingBaseURL(t *testing.T) {
	if got := PairingBaseURL(PairingOptions{}); got != DefaultPairingBaseURL {
		t.Fatalf("default = %q, want %q", got, DefaultPairingBaseURL)
	}
	if got := PairingBaseURL(PairingOptions{Endpoint: "example.com:8443"}); got != "https://example.com:8443/api/v1/pairing/edge" {
		t.Fatalf("legacy endpoint = %q", got)
	}
	if got := PairingBaseURL(PairingOptions{Endpoint: "https://example.com:8443/"}); got != "https://example.com:8443/api/v1/pairing/edge" {
		t.Fatalf("endpoint = %q", got)
	}
	if got := PairingBaseURL(PairingOptions{Endpoint: "https://example.com/api/v1/pairing/edge/"}); got != "https://example.com/api/v1/pairing/edge" {
		t.Fatalf("endpoint with API suffix = %q", got)
	}
	if got := PairingBaseURL(PairingOptions{BaseURL: "https://custom/base/", Endpoint: "ignored"}); got != "https://custom/base" {
		t.Fatalf("baseURL = %q", got)
	}
}

func startPairingTLSServer(t *testing.T, handler http.Handler) (*httptest.Server, string) {
	t.Helper()
	ca := newTestCA(t)
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{ca.issueServer(t, "localhost")}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, writeTempPEM(t, ca.certPEM)
}

func TestFetchPairingConfigSuccess(t *testing.T) {
	server, caFile := startPairingTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pairing/edge/123456" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"config": map[string]interface{}{
					"gateway":    "aabbccddeeff0011",
					"client-id":  "1122334455667788",
					"client-key": "aabbccddeeff00112233445566778899",
					"extras": map[string]interface{}{
						"log-level": "debug",
					},
				},
			},
		})
	}))

	cfg, err := FetchPairingConfig(PairingOptions{
		Pin:     "123-456",
		BaseURL: server.URL + "/api/v1/pairing/edge",
		CAFile:  caFile,
	})
	if err != nil {
		t.Fatalf("FetchPairingConfig: %v", err)
	}
	if cfg.ClientID != "1122334455667788" || cfg.ClientKey == "" || cfg.GatewayID == "" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Extras["log-level"] != "debug" {
		t.Fatalf("unexpected extras: %+v", cfg.Extras)
	}
}

func TestFetchPairingConfigServiceError(t *testing.T) {
	server, caFile := startPairingTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "invalid_pin",
			"details": map[string]interface{}{
				"description": "PIN not found",
			},
		})
	}))

	_, err := FetchPairingConfig(PairingOptions{
		Pin:     "999999",
		BaseURL: server.URL + "/api/v1/pairing/edge",
		CAFile:  caFile,
	})
	if err == nil || err.Error() != "PIN not found" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchPairingConfigHTTPStatusError(t *testing.T) {
	server, caFile := startPairingTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))

	_, err := FetchPairingConfig(PairingOptions{
		Pin:     "123456",
		BaseURL: server.URL + "/api/v1/pairing/edge",
		CAFile:  caFile,
	})
	if err == nil {
		t.Fatal("expected status error")
	}
}

func TestFetchPairingConfigInvalidJSON(t *testing.T) {
	server, caFile := startPairingTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))

	_, err := FetchPairingConfig(PairingOptions{
		Pin:     "123456",
		BaseURL: server.URL + "/api/v1/pairing/edge",
		CAFile:  caFile,
	})
	if err == nil {
		t.Fatal("expected JSON error")
	}
}

func TestFetchPairingConfigEmptyPin(t *testing.T) {
	_, err := FetchPairingConfig(PairingOptions{Pin: "abc"})
	if err == nil {
		t.Fatal("expected empty PIN error")
	}
}

func TestFetchPairingConfigRejectsInsecureEndpoint(t *testing.T) {
	_, err := FetchPairingConfig(PairingOptions{
		Pin:      "123456",
		Endpoint: "http://console.example.com/",
	})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected HTTPS endpoint error, got %v", err)
	}
}

func TestFetchPairingConfigRequiresCustomCA(t *testing.T) {
	server, _ := startPairingTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Without CAFile, system roots alone should reject the test CA.
	_, err := FetchPairingConfig(PairingOptions{
		Pin:     "123456",
		BaseURL: server.URL + "/api/v1/pairing/edge",
	})
	if err == nil {
		t.Fatal("expected TLS verification failure without custom CA")
	}
}
