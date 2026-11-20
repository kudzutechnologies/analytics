/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "protobuf/analytics.pb.h"
#include "protocol.h"
#include "receiver.h"
#include "tests/test_harness.h"

#include <cstring>
#include <string>
#include <vector>

using namespace bs_analytics_test;
using basicstation_analytics::Receiver;

static void FillHdr(struct ana_hdr *h, uint8_t type) {
  std::memset(h, 0, sizeof(*h));
  h->magic = ANA_MAGIC;
  h->version = ANA_VERSION;
  h->type = type;
  h->endian = ANA_ENDIAN;
}

int main() {
  Receiver rx;

  // HELLO then uplink with SF7 + full payload
  {
    struct ana_hello hello = {};
    FillHdr(&hello.hdr, ANA_HELLO);
    hello.gateway_eui = 0x0102030405060708ULL;
    std::memcpy(hello.station_ver, "2.0.6", 5);

    const uint8_t payload[] = {0x40, 0x11, 0x22, 0x33, 0x44, 0x01, 0x00,
                               0x01, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff,
                               0x00, 0x11, 0x22, 0x33, 0x44, 0x55};
    struct ana_uplink up = {};
    FillHdr(&up.hdr, ANA_UPLINK);
    up.rx_wall_us = 1'700'000'000'000'000LL;
    up.rx_gps_time = 1'234'567'890LL;
    up.xtime = 0xAABBCCDDEEFF0011LL;
    up.freq = 868100000;
    up.rssi = -90;
    up.snr_x4 = 34; // 8.5 dB
    up.fts = 123;
    up.dr = 5;
    up.sf = 7;
    up.bw = 0;
    up.crc_status = 1;
    up.size = sizeof(payload);

    std::vector<uint8_t> dgram(ANA_UPLINK_LEN(up.size));
    std::memcpy(dgram.data(), &up, sizeof(up));
    std::memcpy(dgram.data() + sizeof(up), payload, sizeof(payload));

    api::AnalyticsMetrics m;
    Expect(rx.ParseDatagram(reinterpret_cast<const char *>(&hello),
                            sizeof(hello), &m) == 0,
           "hello yields 0 metrics messages");
    Expect(rx.HasGatewayEui(), "HELLO sets gateway EUI");
    Expect(rx.GatewayId().empty(), "ObjectID is not synthesized from EUI");
    Expect(rx.GatewayEuiBytes().size() == 8, "EUI bytes length");
    ExpectEq(rx.GatewayEuiHex(), std::string("0102030405060708"), "EUI hex");

    ExpectEq(rx.ParseDatagram(reinterpret_cast<const char *>(dgram.data()),
                              dgram.size(), &m),
             1, "uplink parsed");
    ExpectEq(m.uplinks_size(), 1, "one uplink");
    const auto &u = m.uplinks(0);
    ExpectEq(static_cast<int>(u.dataratelora().spreadingfactor()),
             static_cast<int>(api::SF7), "SF7");
    ExpectEq(static_cast<int>(u.dataratelora().bandwidth()),
             static_cast<int>(api::BW_125k), "BW125");
    ExpectNear(u.frequency(), 868.1, 0.001, "freq MHz");
    ExpectEq(static_cast<int64_t>(u.rxfinishedtime()),
             static_cast<int64_t>(up.xtime), "rxFinishedTime uses xtime");
    Expect(u.uniqueid().size() == 20, "uniqueId SHA1 length");
    ExpectEq(static_cast<int>(u.fhdr().size()), 10, "variable MAC header");
    ExpectEq(static_cast<int>(u.size()), 20, "payload size");
  }

  // FSK mapping
  {
    Receiver rx2;
    struct ana_hello hello = {};
    FillHdr(&hello.hdr, ANA_HELLO);
    hello.gateway_eui = 1;
    api::AnalyticsMetrics m;
    rx2.ParseDatagram(reinterpret_cast<const char *>(&hello), sizeof(hello),
                      &m);

    struct ana_uplink up = {};
    FillHdr(&up.hdr, ANA_UPLINK);
    up.sf = 0;
    up.bw = 0;
    up.dr = 7;
    up.size = 4;
    uint8_t payload[4] = {0xe0, 2, 3, 4};
    std::vector<uint8_t> dgram(ANA_UPLINK_LEN(4));
    std::memcpy(dgram.data(), &up, sizeof(up));
    std::memcpy(dgram.data() + sizeof(up), payload, 4);
    ExpectEq(rx2.ParseDatagram(reinterpret_cast<const char *>(dgram.data()),
                               dgram.size(), &m),
             1, "fsk uplink");
    ExpectEq(static_cast<int>(m.uplinks(0).modulation()),
             static_cast<int>(api::FSK), "FSK mod");
  }

  // Exact length rejection
  {
    Receiver rx3;
    struct ana_uplink up = {};
    FillHdr(&up.hdr, ANA_UPLINK);
    up.size = 10;
    api::AnalyticsMetrics m;
    ExpectEq(
        rx3.ParseDatagram(reinterpret_cast<const char *>(&up), sizeof(up), &m),
        0, "reject missing payload tail");
    ExpectEq(m.uplinks_size(), 0, "no uplink on truncate");
  }

  // Stat gwTime is microseconds on wire → milliseconds in proto
  {
    Receiver rx4;
    struct ana_hello hello = {};
    FillHdr(&hello.hdr, ANA_HELLO);
    hello.gateway_eui = 2;
    api::AnalyticsMetrics m;
    rx4.ParseDatagram(reinterpret_cast<const char *>(&hello), sizeof(hello),
                      &m);

    struct ana_stat st = {};
    FillHdr(&st.hdr, ANA_STAT);
    st.gw_time_us = 1'700'000'000'000'000LL;
    st.rx_packets = 3;
    st.temperature = 42.5f;
    ExpectEq(
        rx4.ParseDatagram(reinterpret_cast<const char *>(&st), sizeof(st), &m),
        1, "stat");
    ExpectEq(static_cast<int64_t>(m.stats(0).gwtime()),
             static_cast<int64_t>(st.gw_time_us / 1000), "gwTime ms");
    ExpectNear(m.stats(0).gwtemp(), 42.5, 0.01, "temp");
  }

  // Endian reject
  {
    Receiver rx5;
    struct ana_hello hello = {};
    FillHdr(&hello.hdr, ANA_HELLO);
    hello.hdr.endian = 0x04030201u;
    hello.gateway_eui = 3;
    api::AnalyticsMetrics m;
    ExpectEq(rx5.ParseDatagram(reinterpret_cast<const char *>(&hello),
                               sizeof(hello), &m),
             0, "reject endian");
    Expect(!rx5.HasGatewayEui(), "endian fail keeps no gateway");
  }

  return Summary();
}
