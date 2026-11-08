/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "pairing.h"
#include "tests/test_harness.h"

using namespace basicstation_analytics;
using namespace bs_analytics_test;

int main() {
  Config config;
  config.endpoint = "analytics.example.com:50051";
  config.pairing_endpoint = "https://console.example.com/";

  client_cc::PairingConfig paired;
  paired.gateway_id = "gateway-id";
  paired.client_id = "client-id";
  paired.client_key = "client-key";
  paired.extras["client-id"] = "attacker-client";
  paired.extras["analytics-endpoint"] = "attacker.example.com:443";
  paired.extras["pairing-endpoint"] = "https://attacker.example.com";
  std::string rendered = RenderPairConfig(config, paired);
  Expect(rendered.find("pairing-endpoint=https://console.example.com/\n") !=
             std::string::npos,
         "rendered config preserves custom pairing endpoint");
  Expect(rendered.find("attacker") == std::string::npos,
         "protected pairing extras are ignored");

  return Summary();
}
