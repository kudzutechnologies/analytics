#include "semtech_udp.h"
#include "tests/test_harness.h"

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

static const char* kPullReq = "Ar4XAnB2/wBWBgPl";
static const char* kPullAck = "Ar4XBA==";
static const char* kPushDataUp =
    "Ar43AHB2/wBWBgPleyJyeHBrIjpbeyJhZXNrIjowLCJicmQiOjAsImNvZHIiOiI0LzUiLCJkYXRhIjoiUUt5ZEN5"
    "WUFRd01CN2l1NVFENnNINXUxQytZaCIsImRhdHIiOiJTRjlCVzEyNSIsImZyZXEiOjg2Ny4xLCJqdmVyIjoyLCJt"
    "b2R1IjoiTE9SQSIsInJzaWciOlt7ImFudCI6MCwiY2hhbiI6MCwibHNuciI6MTMuMiwicnNzaWMiOi01MH1dLCJz"
    "aXplIjoyMSwic3RhdCI6MSwidGltZSI6IjIwMjMtMDItMjJUMDE6NTM6MzEuMzA2MjI0WiIsInRtc3QiOjM4MDA1"
    "OTUyODR9XX0=";
static const char* kPushAck = "Ar43AQ==";
static const char* kPullResp =
    "AgAEA3sidHhwayI6eyJpbW1lIjpmYWxzZSwidG1zdCI6NDI1NDM3MDM5NiwiZnJlcSI6ODY4LjMsInJmY2giOjAs"
    "InBvd2UiOjE0LCJtb2R1IjoiTE9SQSIsImRhdHIiOiJTRjdCVzEyNSIsImNvZHIiOiI0LzUiLCJpcG9sIjp0cnVl"
    "LCJzaXplIjozMywibmNyYyI6dHJ1ZSwiZGF0YSI6IklHK1NCcGU1TlVvNEk4TDNpQ1RzbUlnWFBFSERMNjNFcWo2"
    "bGFWbXJHS1JGIn19";
static const char* kTxAck = "AgAEBXB2/wBWBgPleyJ0eHBrX2FjayI6eyJlcnJvciI6Ik5PTkUifX0=";

int main() {
  {
    auto raw = B64Decode(kPullReq);
    SemtechUDPMessage msg;
    Expect(DecodeSemtechUDP(raw.data(), raw.size(), msg), "decode pull data");
    ExpectEq(static_cast<int>(msg.kind), static_cast<int>(kPULL_DATA), "PULL_DATA");
    Expect(SemtechUDPIsDownlink(raw.data()), "pull is downlink");
  }
  {
    auto raw = B64Decode(kPullAck);
    SemtechUDPMessage msg;
    Expect(DecodeSemtechUDP(raw.data(), raw.size(), msg), "decode pull ack");
    ExpectEq(static_cast<int>(msg.kind), static_cast<int>(kPULL_ACK), "PULL_ACK");
  }
  {
    auto raw = B64Decode(kPushDataUp);
    SemtechUDPMessage msg;
    Expect(DecodeSemtechUDP(raw.data(), raw.size(), msg), "decode push up");
    ExpectEq(static_cast<int>(msg.kind), static_cast<int>(kPUSH_DATA), "PUSH_DATA");
    Expect(SemtechUDPIsUplink(raw.data()), "push is uplink");
    SemtechJsonPayload payload;
    Expect(ParseSemtechJsonPayload(msg, payload), "parse json");
    ExpectEq(payload.rxpk.size(), size_t{1}, "one rxpk");
    ExpectEq(payload.rxpk[0].datr, "SF9BW125", "datr");
    ExpectEq(payload.rxpk[0].rsig.size(), size_t{1}, "rsig");
    ExpectNear(payload.rxpk[0].rsig[0].lsnr, 13.2, 1e-6, "lsnr");
  }
  {
    auto raw = B64Decode(kPushAck);
    SemtechUDPMessage msg;
    Expect(DecodeSemtechUDP(raw.data(), raw.size(), msg), "decode push ack");
    ExpectEq(static_cast<int>(msg.kind), static_cast<int>(kPUSH_ACK), "PUSH_ACK");
  }
  {
    auto raw = B64Decode(kPullResp);
    SemtechUDPMessage msg;
    Expect(DecodeSemtechUDP(raw.data(), raw.size(), msg), "decode pull resp");
    ExpectEq(static_cast<int>(msg.kind), static_cast<int>(kPULL_RESP), "PULL_RESP");
    SemtechJsonPayload payload;
    Expect(ParseSemtechJsonPayload(msg, payload), "parse txpk");
    Expect(payload.txpk.has_value(), "has txpk");
    ExpectEq(payload.txpk->datr, "SF7BW125", "tx datr");
    ExpectNear(static_cast<double>(payload.txpk->frequency), 868.3, 1e-3, "tx freq");
  }
  {
    auto raw = B64Decode(kTxAck);
    SemtechUDPMessage msg;
    Expect(DecodeSemtechUDP(raw.data(), raw.size(), msg), "decode tx ack");
    ExpectEq(static_cast<int>(msg.kind), static_cast<int>(kTX_ACK), "TX_ACK");
  }

  return Summary();
}
