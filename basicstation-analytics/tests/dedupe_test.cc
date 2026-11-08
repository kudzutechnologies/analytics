/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "dedupe.h"
#include "protobuf/analytics.pb.h"
#include "tests/test_harness.h"

using namespace bs_analytics_test;
using namespace basicstation_analytics;

int main() {
  unsigned char payload[] = {0x40, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07};
  api::AnalyticsUplink up;
  up.set_frequency(868.1f);
  up.set_codingrate(api::CR_4_5);
  auto *lora = up.mutable_dataratelora();
  lora->set_spreadingfactor(api::SF7);
  lora->set_bandwidth(api::BW_125k);
  ComputeUniqueIdUp(&up, payload, sizeof(payload));
  Expect(up.uniqueid().size() == 20, "sha1 len");

  api::AnalyticsUplink up2;
  up2.CopyFrom(up);
  up2.clear_uniqueid();
  ComputeUniqueIdUp(&up2, payload, sizeof(payload));
  ExpectEq(up.uniqueid(), up2.uniqueid(), "deterministic");
  return Summary();
}
