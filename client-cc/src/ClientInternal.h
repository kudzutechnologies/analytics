#pragma once

#include <string>

namespace client_cc {

// Reproduces the deployed Go protocol's sha256.New().Sum(input) behavior:
// input followed by the SHA-256 digest of an empty message.
std::string BuildLoginHash(const std::string& challenge,
                           const std::string& key_bytes);

}  // namespace client_cc
