#include "dedupe.h"
#include "protobuf/analytics.pb.h"
#include "tests/test_harness.h"

using namespace forwarder_cc;
using namespace forwarder_cc_test;

int main() {
  // Minimal payload with LoRaWAN MHDR+FHDR enough for FHDR extraction.
  unsigned char payload[20] = {
      0x40,  // unconfirmed uplink
      0x01, 0x02, 0x03, 0x04,  // DevAddr
      0x00,  // FCtrl
      0x10, 0x00,  // FCnt
      // FPort + FRMPayload
      0x01, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22, 0x33, 0x44,
  };

  api::AnalyticsUplink up;
  up.set_frequency(868.1f);
  up.set_modulation(api::LORA);
  auto* lora = up.mutable_dataratelora();
  lora->set_spreadingfactor(api::SF7);
  lora->set_bandwidth(api::BW_125k);
  up.set_codingrate(api::CR_4_5);
  auto* ant = up.add_ant();
  ant->set_lsnr(5.0f);

  ComputeUniqueIdUp(&up, payload, sizeof(payload));
  Expect(up.uniqueid().size() == 20, "up unique id is SHA1 (20 bytes)");

  api::AnalyticsDownlink down;
  down.set_frequency(868.1f);
  down.set_modulation(api::LORA);
  auto* dl = down.mutable_dataratelora();
  dl->set_spreadingfactor(api::SF7);
  dl->set_bandwidth(api::BW_125k);
  down.set_codingrate(api::CR_4_5);
  ComputeUniqueIdDown(&down, payload, sizeof(payload));
  Expect(down.uniqueid().size() == 20, "down unique id is SHA1 (20 bytes)");

  // Deterministic: same input → same id
  api::AnalyticsUplink up2;
  up2.CopyFrom(up);
  up2.clear_uniqueid();
  ComputeUniqueIdUp(&up2, payload, sizeof(payload));
  ExpectEq(up.uniqueid(), up2.uniqueid(), "deterministic unique id");

  return Summary();
}
