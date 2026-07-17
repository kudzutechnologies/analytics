#pragma once

#include <string>

namespace client_cc {

// Builds a PEM bundle of trust anchors for gRPC:
// - If ca_file is empty, out_pem is left empty so gRPC uses its platform defaults
//   (system roots / GRPC_DEFAULT_SSL_ROOTS_FILE_PATH).
// - If ca_file is set, out_pem contains system default roots plus the CA file PEM.
// Returns false and sets error_msg on failure.
bool LoadCombinedRootCertsPEM(const std::string& ca_file,
                              std::string& out_pem,
                              std::string& error_msg);

// Configures an OpenSSL SSL_CTX with system default verify paths and optionally
// an extra CA file. Enables peer certificate verification.
// Returns false and sets error_msg on failure.
bool ConfigureSSLContextTrust(void* ssl_ctx,
                              const std::string& ca_file,
                              std::string& error_msg);

}  // namespace client_cc
