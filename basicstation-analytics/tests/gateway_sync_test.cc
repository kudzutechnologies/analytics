/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "gateway_sync.h"
#include "tests/test_harness.h"

using namespace bs_analytics_test;
using namespace basicstation_analytics;

int main() {
  Config c;
  ParsedGatewaySync parsed;
  std::string err;

  Expect(!ParseGatewaySyncConfig(c, parsed, err), "no sync options");
  Expect(err.empty(), "no error when absent");

  c.gateway_location = "45.0,9.0,100";
  Expect(!ParseGatewaySyncConfig(c, parsed, err), "missing eid/eui");
  Expect(!err.empty(), "error when incomplete");

  c.gateway_eui = "0102030405060708";
  c.gateway_id = "aaaaaaaaaaaaaaaaaaaaaaaa";
  Expect(ParseGatewaySyncConfig(c, parsed, err), "valid sync");
  Expect(parsed.has_any, "has_any");
  Expect(parsed.has_location, "has_location");
  ExpectEq(parsed.eui.size(), size_t{8}, "eui bytes");

  auto req = ToUpsertRequest(parsed);
  Expect(req.gateway_id() == c.gateway_id, "upsert gateway_id");
  Expect(req.has_location(), "upsert location");
  return Summary();
}
