#include "Client.h"
#include "forwarder.h"
#include "semtech_udp.h"
#include "tests/test_harness.h"
#include "udp_proxy.h"

#include <arpa/inet.h>
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
  fwd.TestUpLocalData(dn.data(), dn.size(), ep);
  ExpectEq(fwd.TestDownlinkCount(), 1, "downlink via up path");

  auto with_etime = MakePushWithETime();
  SockAddr4 ep2 = ep;
  ep2.port = htons(40001);  // same IP → same frame key
  fwd.TestUpLocalData(with_etime.data(), with_etime.size(), ep2);
  ExpectEq(fwd.TestFirstAntennaETime(), "YWJj", "etime propagated");

  // IP-only keying: different ports, same IP → single frame
  ExpectEq(fwd.TestFrameCount(), size_t{1}, "IP-only frame key");

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
