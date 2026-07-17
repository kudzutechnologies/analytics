#pragma once

#include <map>
#include <string>

namespace client_cc {

inline constexpr const char* kDefaultPairingBaseURL =
    "https://eu1.cluster.kudzu.gr/api/v1/pairing/edge";

struct PairingOptions {
  // Pairing PIN. Non-digit characters are stripped before use.
  std::string pin;
  // Pairing API base URL without the trailing PIN segment.
  // When empty, endpoint or kDefaultPairingBaseURL is used.
  std::string base_url;
  // Optional analytics host[:port]. When set and base_url is empty, the pairing
  // URL becomes https://{host}/api/v1/pairing/edge (port stripped from host).
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

// Keep only ASCII digits from pin.
std::string NormalizePairingPin(const std::string& pin);

// Resolve the pairing base URL from options.
std::string PairingBaseURL(const PairingOptions& opts);

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
