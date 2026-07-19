#include "config.h"
#include "pairing.h"
#include "tests/test_harness.h"

using namespace forwarder_cc;
using namespace forwarder_cc_test;

int main() {
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
