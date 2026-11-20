/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "protocol.h"
#include "tests/test_harness.h"

#include <cstring>

using namespace bs_analytics_test;

int main() {
  ExpectEq(sizeof(struct ana_hdr), size_t{8}, "ana_hdr size");
  ExpectEq(sizeof(struct ana_hello), size_t{24}, "ana_hello size");
  ExpectEq(sizeof(struct ana_uplink), size_t{50}, "ana_uplink size");
  ExpectEq(sizeof(struct ana_downlink), size_t{40}, "ana_downlink size");
  ExpectEq(sizeof(struct ana_stat), size_t{56}, "ana_stat size");

  ExpectEq(ANA_UPLINK_LEN(0), sizeof(struct ana_uplink),
           "empty uplink datagram");
  ExpectEq(ANA_UPLINK_LEN(20), sizeof(struct ana_uplink) + 20, "uplink+20");
  ExpectEq(ANA_DOWNLINK_LEN(30), sizeof(struct ana_downlink) + 30,
           "downlink+30");

  struct ana_hdr h = {};
  h.magic = ANA_MAGIC;
  h.version = ANA_VERSION;
  h.type = ANA_HELLO;
  h.endian = ANA_ENDIAN;
  Expect(ana_hdr_ok(&h), "valid hdr");

  h.endian = 0x04030201u;
  Expect(!ana_hdr_ok(&h), "reject swapped endian marker");

  h.endian = ANA_ENDIAN;
  h.magic = 0;
  Expect(!ana_hdr_ok(&h), "reject bad magic");

  return Summary();
}
