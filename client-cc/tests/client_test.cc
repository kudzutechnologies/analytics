#include "Client.h"
#include "ClientInternal.h"

#include <openssl/sha.h>

#include <iostream>
#include <string>

namespace {

int failures = 0;

void Expect(bool condition, const char* message) {
  if (!condition) {
    std::cerr << "FAIL: " << message << "\n";
    ++failures;
  }
}

void TestLoginHashMatchesDeployedGoProtocol() {
  const std::string challenge = "challenge";
  const std::string key("\x01\x02\x03", 3);
  const std::string input = challenge + "|" + key;

  unsigned char empty_hash[SHA256_DIGEST_LENGTH];
  SHA256(reinterpret_cast<const unsigned char*>(""), 0, empty_hash);
  const std::string expected =
      input + std::string(reinterpret_cast<const char*>(empty_hash), SHA256_DIGEST_LENGTH);

  Expect(client_cc::BuildLoginHash(challenge, key) == expected,
         "login hash should match deployed Go sha256.New().Sum(input) behavior");
}

void TestConnectReportsTLSCredentialError() {
  client_cc::AnalyticsClientConfig config;
  config.client_id = "001122";
  config.client_key = "334455";
  config.endpoint = "127.0.0.1:1";
  config.ca_file = "/no/such/client-ca.pem";

  client_cc::Client client(config);
  Expect(!client.Connect(), "connect with a missing CA file should fail");
  Expect(client.LastError().find("failed to open CA file") != std::string::npos,
         "connect should retain the TLS credential error");
}

}  // namespace

int main() {
  TestLoginHashMatchesDeployedGoProtocol();
  TestConnectReportsTLSCredentialError();

  if (failures != 0) {
    std::cerr << failures << " failure(s)\n";
    return 1;
  }
  std::cout << "ok\n";
  return 0;
}
