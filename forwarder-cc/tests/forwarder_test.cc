#include "Client.h"
#include "forwarder.h"
#include "semtech_udp.h"
#include "tests/test_harness.h"
#include "udp_proxy.h"

#include <arpa/inet.h>
#include <chrono>
#include <cstring>
#include <openssl/evp.h>
#include <string>
#include <vector>

using namespace forwarder_cc;
using namespace forwarder_cc_test;

static std::vector<uint8_t> B64Decode(const std::string& in) {
  std::string padded = in;
  while (padded.size() % 4) padded += '=';
  std::vector<unsigned char> out((padded.size() / 4) * 3 + 4);
  int len = EVP_DecodeBlock(out.data(), reinterpret_cast<const unsigned char*>(padded.data()),
                            static_cast<int>(padded.size()));
  if (len <= 0) return {};
  size_t pad = 0;
  if (!in.empty() && in.back() == '=') pad = (in.size() >= 2 && in[in.size() - 2] == '=') ? 2 : 1;
  return std::vector<uint8_t>(out.begin(), out.begin() + (len - static_cast<int>(pad)));
}

static const char* kPushDataUp =
    "Ar43AHB2/wBWBgPleyJyeHBrIjpbeyJhZXNrIjowLCJicmQiOjAsImNvZHIiOiI0LzUiLCJkYXRhIjoiUUt5ZEN5"
    "WUFRd01CN2l1NVFENnNINXUxQytZaCIsImRhdHIiOiJTRjlCVzEyNSIsImZyZXEiOjg2Ny4xLCJqdmVyIjoyLCJt"
    "b2R1IjoiTE9SQSIsInJzaWciOlt7ImFudCI6MCwiY2hhbiI6MCwibHNuciI6MTMuMiwicnNzaWMiOi01MH1dLCJz"
    "aXplIjoyMSwic3RhdCI6MSwidGltZSI6IjIwMjMtMDItMjJUMDE6NTM6MzEuMzA2MjI0WiIsInRtc3QiOjM4MDA1"
    "OTUyODR9XX0=";

static const char* kPullResp =
    "AgAEA3sidHhwayI6eyJpbW1lIjpmYWxzZSwidG1zdCI6NDI1NDM3MDM5NiwiZnJlcSI6ODY4LjMsInJmY2giOjAs"
    "InBvd2UiOjE0LCJtb2R1IjoiTE9SQSIsImRhdHIiOiJTRjdCVzEyNSIsImNvZHIiOiI0LzUiLCJpcG9sIjp0cnVl"
    "LCJzaXplIjozMywibmNyYyI6dHJ1ZSwiZGF0YSI6IklHK1NCcGU1TlVvNEk4TDNpQ1RzbUlnWFBFSERMNjNFcWo2"
    "bGFWbXJHS1JGIn19";

// Synthetic PUSH_DATA with etime in rsig.
static std::vector<uint8_t> MakePushWithETime() {
  const char* json =
      "{\"rxpk\":[{\"tmst\":1,\"freq\":868.1,\"stat\":1,\"modu\":\"LORA\",\"datr\":\"SF7BW125\","
      "\"codr\":\"4/5\",\"size\":1,\"data\":\"AA==\",\"rsig\":[{\"ant\":0,\"chan\":0,\"rssic\":-50,"
      "\"lsnr\":1.0,\"etime\":\"YWJj\"}]}]}";
  std::vector<uint8_t> pkt;
  pkt.push_back(2);  // version
  pkt.push_back(0);
  pkt.push_back(1);  // token
  pkt.push_back(kPUSH_DATA);
  for (int i = 0; i < 8; ++i) pkt.push_back(static_cast<uint8_t>(i + 1));
  pkt.insert(pkt.end(), reinterpret_cast<const uint8_t*>(json),
             reinterpret_cast<const uint8_t*>(json) + std::strlen(json));
  return pkt;
}

static std::vector<uint8_t> MakeTimestampPush(const std::string& timestamp) {
  const std::string json = "{\"rxpk\":[{\"time\":\"" + timestamp +
      "\",\"tmst\":4294967290,\"tmms\":1384035218123,\"freq\":868.1,\"stat\":1,"
      "\"modu\":\"LORA\",\"datr\":\"SF7BW125\",\"codr\":\"4/5\",\"size\":21,"
      "\"data\":\"QKydCyYAQwMB7iu5QD6sH5u1C+Yh\"}],\"stat\":{\"time\":\"" + timestamp + "\"}}";
  std::vector<uint8_t> pkt{2, 0, 1, kPUSH_DATA, 1, 2, 3, 4, 5, 6, 7, 8};
  pkt.insert(pkt.end(), json.begin(), json.end());
  return pkt;
}

int main() {
  Config cfg;
  cfg.gateway_id = "gw1";
  cfg.max_udp_streams = 2;
  cfg.server_side = true;
  cfg.flush_interval = 60;

  client_cc::AnalyticsClientConfig cc;
  cc.client_id = "id";
  cc.client_key = "00";
  cc.endpoint = "127.0.0.1:1";
  client_cc::Client client(cc);

  UdpProxyConfig pc;
  pc.listen_host = "127.0.0.1";
  pc.listen_port_up = 0;  // unused — we drive handlers directly
  pc.connect_host = "127.0.0.1";
  UdpProxy proxy(pc);

  const struct {
    const char* name;
    const char* input;
    int64_t micros, millis;
  } timestamp_cases[] = {
      {"UTC fractional timestamp units", "2023-11-14T22:13:20.123456789Z", 1700000000123456, 1700000000123},
      {"positive UTC offset", "2023-11-15T00:13:20.123456789+02:00", 1700000000123456, 1700000000123},
      {"negative UTC offset", "2023-11-14T15:13:20.123456789-07:00", 1700000000123456, 1700000000123},
      {"whole seconds", "2023-11-14T22:13:20Z", 1700000000000000, 1700000000000},
      {"short fraction", "2023-11-14T22:13:20.1Z", 1700000000100000, 1700000000100},
      {"missing timestamp", "", 0, 0},
      {"invalid calendar date", "2023-02-29T22:13:20Z", 0, 0},
      {"trailing text", "2023-11-14T22:13:20Zjunk", 0, 0},
      {"invalid offset", "2023-11-14T22:13:20+AA:00", 0, 0},
  };
  for (const auto& tc : timestamp_cases) {
    AnalyticsForwarder parser(cfg, &client, &proxy);
    const auto packet = MakeTimestampPush(tc.input);
    SockAddr4 source;
    source.addr = htonl(0x7f000001);
    source.port = htons(40000);
    parser.TestUpLocalData(packet.data(), packet.size(), source);
    api::AnalyticsMetrics clocks;
    Expect(clocks.ParseFromString(parser.TestFirstFrame()), "parse timestamp fixture frame");
    Expect(clocks.uplinks_size() == 1 && clocks.stats_size() == 1, "timestamp fixture observations");
    if (clocks.uplinks_size() == 1 && clocks.stats_size() == 1) {
      Expect(clocks.uplinks(0).rxwalltime() == tc.micros, tc.name);
      Expect(clocks.stats(0).gwtime() == tc.millis, tc.name);
      Expect(clocks.uplinks(0).rxfinishedtime() == 4294967290LL, "concentrator microseconds unchanged");
      Expect(clocks.uplinks(0).rxgpstime() == 1384035218123LL, "GPS epoch milliseconds unchanged");
    }
  }

  AnalyticsForwarder fwd(cfg, &client, &proxy);

  SockAddr4 ep;
  ep.addr = htonl(0x7f000001);
  ep.port = htons(40000);

  auto up = B64Decode(kPushDataUp);
  fwd.TestUpLocalData(up.data(), up.size(), ep);
  ExpectEq(fwd.TestUplinkCount(), 1, "uplink counted");
  ExpectEq(static_cast<int>(fwd.TestFirstUplinkSF()), static_cast<int>(api::SF9),
           "SF9 parsed from SF9BW125");

  // Shared-port style: downlink arrives on up handler.
  auto dn = B64Decode(kPullResp);
  const auto before_downlink = std::chrono::duration_cast<std::chrono::milliseconds>(
      std::chrono::system_clock::now().time_since_epoch()).count();
  fwd.TestUpLocalData(dn.data(), dn.size(), ep);
  ExpectEq(fwd.TestDownlinkCount(), 1, "downlink via up path");

  api::AnalyticsMetrics captured;
  Expect(captured.ParseFromString(fwd.TestFirstFrame()), "capture analytics frame");
  const auto expected_up = B64Decode("QKydCyYAQwMB7iu5QD6sH5u1C+Yh");
  const auto expected_down = B64Decode("IG+SBpe5NUo4I8L3iCTsmIgXPEHDL63Eqj6laVmrGKRF");
  ExpectEq(captured.uplinks(0).phypayload(),
           std::string(expected_up.begin(), expected_up.end()), "complete uplink includes MIC");
  ExpectEq(captured.downlinks(0).phypayload(),
           std::string(expected_down.begin(), expected_down.end()), "complete downlink includes MIC");
  ExpectEq(captured.uplinks(0).fhdr().size(), size_t{9}, "legacy uplink header retained");
  Expect(captured.downlinks(0).txtime() == 4254370396LL, "TX concentrator microseconds unchanged");
  const auto after_downlink = std::chrono::duration_cast<std::chrono::milliseconds>(
      std::chrono::system_clock::now().time_since_epoch()).count();
  Expect(captured.downlinks(0).rxwalltime() >= before_downlink &&
         captured.downlinks(0).rxwalltime() <= after_downlink,
         "command observation uses UTC milliseconds");

  auto with_etime = MakePushWithETime();
  SockAddr4 ep2 = ep;
  ep2.port = htons(40001);  // same IP, a distinct gateway endpoint
  fwd.TestUpLocalData(with_etime.data(), with_etime.size(), ep2);
  ExpectEq(fwd.TestFirstAntennaETime(), "YWJj", "etime propagated");

  // Distinct UDP peers sharing an IP must retain separate gateway batches.
  ExpectEq(fwd.TestFrameCount(), size_t{2}, "endpoint-scoped frame key");

  // Eviction: add frames from different IPs beyond max_udp_streams
  SockAddr4 ep3;
  ep3.addr = htonl(0x7f000002);
  ep3.port = htons(1);
  fwd.TestUpLocalData(up.data(), up.size(), ep3);
  SockAddr4 ep4;
  ep4.addr = htonl(0x7f000003);
  ep4.port = htons(1);
  fwd.TestUpLocalData(up.data(), up.size(), ep4);
  Expect(fwd.TestFrameCount() <= 2, "bounded frames");

  return Summary();
}
