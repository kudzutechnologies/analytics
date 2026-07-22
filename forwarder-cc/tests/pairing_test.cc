#include "config.h"
#include "pairing.h"
#include "tests/test_harness.h"

using namespace forwarder_cc;
using namespace forwarder_cc_test;

int main() {
  Config endpoints;
  endpoints.endpoint = "analytics.example.com:50051";
  endpoints.pairing_endpoint = "https://console.example.com/";
  client_cc::PairingConfig paired;
  paired.gateway_id = "gateway-id";
  paired.client_id = "client-id";
  paired.client_key = "client-key";
  paired.extras["client-id"] = "attacker-client";
  paired.extras["analytics-endpoint"] = "attacker.example.com:443";
  paired.extras["pairing-endpoint"] = "https://attacker.example.com";
  std::string rendered = RenderPairConfig(endpoints, paired);
  Expect(rendered.find("pairing-endpoint=https://console.example.com/\n") !=
             std::string::npos,
         "rendered config preserves custom pairing endpoint");
  Expect(rendered.find("attacker") == std::string::npos,
         "protected pairing extras are ignored");

  // Pairing with invalid PIN should fail without network for empty digits...
  Config c;
  c.endpoint = "ingress.eu1.cluster.kudzu.gr:443";
  std::string err;
  std::string ini = GetRenderedPairConfig("abc", c, err);
  Expect(ini.empty(), "non-digit pin fails");
  Expect(!err.empty(), "error message set");

  // Render path is covered indirectly; ensure defaults leave gateway-* empty in DefaultConfig
  // by checking that a successful pair isn't required for this smoke test.
  ExpectEq(std::string(kForwarderVersion), "0.1.12", "version constant");

  return Summary();
}
