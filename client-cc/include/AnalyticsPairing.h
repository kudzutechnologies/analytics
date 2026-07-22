#pragma once

#include <map>
#include <string>

namespace client_cc {

// Default pairing API base URL without the trailing PIN segment.
inline constexpr const char* kDefaultPairingBaseURL =
    "https://console.eu1.cluster.kudzu.gr/api/v1/pairing/edge";

struct PairingOptions {
  // Pairing PIN. Non-digit characters are stripped before use.
  std::string pin;
  // Pairing API base URL without the trailing PIN segment.
  // When empty, endpoint or kDefaultPairingBaseURL is used.
  std::string base_url;
  // Optional pairing HTTPS origin/base URL. Legacy analytics host:port values
  // remain supported (the analytics port is discarded). base_url takes
  // precedence.
  std::string endpoint;
  // Optional PEM CA file appended to the system trust store.
  std::string ca_file;
};

struct PairingConfig {
  std::string gateway_id;
  std::string client_id;
  std::string client_key;
  std::map<std::string, std::string> extras;
};

// Return true when a pairing response extra must not replace local identity
// or transport configuration.
bool IsProtectedPairingConfigKey(const std::string& key);

// Keep only ASCII digits from pin.
std::string NormalizePairingPin(const std::string& pin);

// Resolve the pairing base URL from options.
std::string PairingBaseURL(const PairingOptions& opts);

// Return the default HTTPS pairing origin without the API path.
std::string DefaultPairingEndpoint();

// Parse a pairing JSON response body into PairingConfig.
// Returns false and sets error_msg on failure.
bool ParsePairingResponseJSON(const std::string& body,
                              PairingConfig& out,
                              std::string& error_msg);

// Download edge pairing credentials over HTTPS using additive TLS trust
// (system roots, plus ca_file when provided).
// Returns false and sets error_msg on failure.
bool FetchPairingConfig(const PairingOptions& opts,
                        PairingConfig& out,
                        std::string& error_msg);

}  // namespace client_cc
