#pragma once

#include "config.h"
#include <string>

namespace forwarder_cc {

// Fetches pairing config via client_cc::FetchPairingConfig and returns a
// rendered INI string (non-default keys only). Uses additive TLS trust from
// current_config.ca_file. PIN non-digits are stripped.
// On error returns empty string and sets error_msg.
std::string GetRenderedPairConfig(const std::string& pin,
                                  const Config& current_config,
                                  std::string& error_msg);

}  // namespace forwarder_cc
