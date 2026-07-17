#include "AnalyticsPairing.h"

#include <iostream>
#include <string>

namespace {

int failures = 0;

void Expect(bool cond, const char* msg) {
  if (!cond) {
    std::cerr << "FAIL: " << msg << "\n";
    ++failures;
  }
}

void ExpectEq(const std::string& got, const std::string& want, const char* msg) {
  if (got != want) {
    std::cerr << "FAIL: " << msg << " got=[" << got << "] want=[" << want << "]\n";
    ++failures;
  }
}

}  // namespace

int main() {
  using namespace client_cc;

  ExpectEq(NormalizePairingPin("123-456"), "123456", "normalize dashes");
  ExpectEq(NormalizePairingPin(" 12 34 "), "1234", "normalize spaces");
  ExpectEq(NormalizePairingPin("abc"), "", "normalize letters");

  ExpectEq(PairingBaseURL(PairingOptions{}), kDefaultPairingBaseURL, "default base URL");

  PairingOptions with_endpoint;
  with_endpoint.endpoint = "example.com:50051";
  ExpectEq(PairingBaseURL(with_endpoint),
           "https://example.com/api/v1/pairing/edge", "endpoint strips port");

  PairingOptions with_base;
  with_base.base_url = "https://custom/base/";
  with_base.endpoint = "ignored";
  ExpectEq(PairingBaseURL(with_base), "https://custom/base", "explicit base URL wins");

  PairingConfig cfg;
  std::string err;
  Expect(ParsePairingResponseJSON(R"({
      "data": {
        "config": {
          "gateway": "aabbccddeeff0011",
          "client-id": "1122334455667788",
          "client-key": "deadbeef",
          "extras": {"log-level": "debug", "buffer-size": 1500}
        }
      }
    })",
                                  cfg, err),
         "parse success");
  ExpectEq(cfg.client_id, "1122334455667788", "client-id");
  ExpectEq(cfg.client_key, "deadbeef", "client-key");
  ExpectEq(cfg.gateway_id, "aabbccddeeff0011", "gateway");
  ExpectEq(cfg.extras["log-level"], "debug", "string extra");
  ExpectEq(cfg.extras["buffer-size"], "1500", "numeric extra dump");

  Expect(!ParsePairingResponseJSON(
             R"({"error":"invalid_pin","details":{"description":"PIN not found"}})", cfg,
             err),
         "service error");
  ExpectEq(err, "PIN not found", "service error description");

  Expect(!ParsePairingResponseJSON("not-json", cfg, err), "invalid JSON");
  Expect(!ParsePairingResponseJSON(R"({"data":{}})", cfg, err), "missing config");

  PairingConfig unused;
  PairingOptions bad_pin;
  bad_pin.pin = "abc";
  Expect(!FetchPairingConfig(bad_pin, unused, err), "empty pin rejected");
  ExpectEq(err, "pairing PIN must contain digits", "empty pin message");

  if (failures) {
    std::cerr << failures << " failure(s)\n";
    return 1;
  }
  std::cout << "ok\n";
  return 0;
}
