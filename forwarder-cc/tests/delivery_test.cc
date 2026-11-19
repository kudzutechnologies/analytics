#include "Client.h"
#include "forwarder.h"
#include "downlink_delivery.h"
#include "tests/test_harness.h"
#include <arpa/inet.h>
#include <fstream>
#include <iterator>
#include <thread>
#include <google/protobuf/util/message_differencer.h>
#include <vector>

using namespace forwarder_cc;
using namespace forwarder_cc_test;

std::vector<uint8_t> Packet(uint8_t kind, const std::string& body) {
  std::vector<uint8_t> result{2, 0x34, 0x12, kind};
  if (kind == kPULL_DATA || kind == kTX_ACK)
    for (uint8_t i = 1; i <= 8; ++i) result.push_back(i);
  result.insert(result.end(), body.begin(), body.end());
  return result;
}
int main() {
  SockAddr4 peer;
  peer.addr = htonl(0x7f000001); peer.port = htons(1700);
  Config cfg; cfg.max_udp_streams = 10; cfg.server_side = false;
  client_cc::AnalyticsClientConfig cc; cc.endpoint = "127.0.0.1:1";
  client_cc::Client client(cc);
  UdpProxyConfig pc; UdpProxy proxy(pc);
  AnalyticsForwarder fwd(cfg, &client, &proxy);
  auto poll = Packet(kPULL_DATA, "");
  fwd.TestDnLocalData(poll.data(), poll.size(), peer);
  const std::string body = R"json({"txpk":{"tmst":42,"freq":868.3,"rfch":1,"powe":14,"modu":"LORA","datr":"SF7BW125","codr":"4/5","size":13,"data":"YAQDAgEAAQABAAAAAA=="}})json";
  auto command = Packet(kPULL_RESP, body);
  fwd.TestDnLocalData(command.data(), command.size(), peer);
  fwd.TestDnLocalData(command.data(), command.size(), peer);
  api::AnalyticsMetrics frame;
  Expect(frame.ParseFromString(fwd.TestFirstFrame()), "parse frame");
  ExpectEq(frame.downlinks_size(), 2, "command observations preserved");
  auto metadata = frame.downlinks(0).delivery();
  ExpectEq(metadata.id(), std::string("eui:0102030405060708:1234"), "gateway/token identity");
  ExpectEq(metadata.commandkey(), frame.downlinks(1).delivery().commandkey(), "replay fingerprint stable");
  ExpectEq(frame.downlinks(0).phypayload().size(), size_t{13}, "candidate packet retained");
  struct Case { std::string body; api::DownlinkOutcomeStatus status; std::string reason; };
  for (const auto& tc : std::vector<Case>{
      {"", api::OUTCOME_ACCEPTED, ""},
      {R"({"txpk_ack":{"error":"NONE"}})", api::OUTCOME_ACCEPTED, ""},
      {R"({"txpk_ack":{"error":"TOO_LATE"}})", api::OUTCOME_REJECTED, "TOO_LATE"},
      {R"({"txpk_ack":{"error":"NEW_REJECTION"}})", api::OUTCOME_REJECTED, "NEW_REJECTION"},
      {R"({"txpk_ack":{}})", api::OUTCOME_UNKNOWN, "ack_invalid"},
      {R"({"txpk_ack":{"error":null}})", api::OUTCOME_UNKNOWN, "ack_invalid"},
      {R"({"txpk_ack":{"error":1}})", api::OUTCOME_UNKNOWN, "ack_invalid"},
      {"{", api::OUTCOME_UNKNOWN, "ack_invalid"}}) {
    auto packet = Packet(kTX_ACK, tc.body);
    fwd.TestDnLocalData(packet.data(), packet.size(), peer);
    Expect(frame.ParseFromString(fwd.TestFirstFrame()), "snapshot decodes");
    const auto& ack = frame.downlinkoutcomes(frame.downlinkoutcomes_size() - 1);
    ExpectEq(ack.id(), metadata.id(), "ACK identity matches command");
    ExpectEq(ack.status(), tc.status, "explicit ACK outcome");
    ExpectEq(ack.reason(), tc.reason, "native error retained");
  }
  Expect(frame.ParseFromString(fwd.TestTakeFirstFrame()), "taken snapshot decodes");
  ExpectEq(frame.downlinkoutcomes_size(), 8, "flush snapshot contains outcomes");
  Expect(frame.ParseFromString(fwd.TestFirstFrame()), "snapshot decodes");
  ExpectEq(frame.downlinkoutcomes_size(), 0, "outcomes reset after flush");
  ExpectEq(frame.downlinks_size(), 0, "commands reset after flush");
  uint8_t truncated[]{2, 0, 0, kTX_ACK};
  fwd.TestDnLocalData(truncated, sizeof(truncated), peer);
  Expect(frame.ParseFromString(fwd.TestFirstFrame()), "snapshot decodes");
  ExpectEq(frame.downlinkoutcomes_size(), 0, "truncated ACK is not success");

  std::ifstream input(ANALYTICS_DELIVERY_FIXTURE, std::ios::binary);
  std::string wire((std::istreambuf_iterator<char>(input)), std::istreambuf_iterator<char>());
  api::AnalyticsMetrics golden;
  Expect(!wire.empty() && golden.ParseFromString(wire), "Go wire fixture decodes in C++");
  api::AnalyticsMetrics roundtrip;
  Expect(roundtrip.ParseFromString(golden.SerializeAsString()), "C++ round trip decodes");
  Expect(google::protobuf::util::MessageDifferencer::Equals(golden, roundtrip), "Go/C++ round trip retains every field");
  ExpectEq(metadata.commandkey(), golden.downlinks(0).delivery().commandkey(), "Go/C++ command fingerprint parity");

  auto ack = Packet(kTX_ACK, "");
  std::vector<std::thread> workers;
  for (int i = 0; i < 4; ++i) workers.emplace_back([&] {
    for (int n = 0; n < 100; ++n) fwd.TestDnLocalData(ack.data(), ack.size(), peer);
  });
  int count = 0;
  for (int i = 0; i < 100; ++i) {
    Expect(frame.ParseFromString(fwd.TestTakeFirstFrame()), "taken snapshot decodes"); count += frame.downlinkoutcomes_size();
  }
  for (auto& worker : workers) worker.join();
  Expect(frame.ParseFromString(fwd.TestTakeFirstFrame()), "taken snapshot decodes"); count += frame.downlinkoutcomes_size();
  ExpectEq(count, 400, "concurrent snapshots retain every ACK exactly once");
  fwd.TestDnLocalData(command.data(), command.size(), peer);
  auto changed = poll; changed[4] = 99;
  fwd.TestDnLocalData(changed.data(), changed.size(), peer);
  Expect(frame.ParseFromString(fwd.TestFirstFrame()), "changed gateway envelope decodes");
  ExpectEq(frame.downlinks_size(), 0, "new gateway cannot inherit old queued commands");
  ExpectEq(static_cast<unsigned char>(frame.gatewayeui()[0]), static_cast<unsigned char>(99), "new gateway replaces envelope after flush");
  auto fsk = Packet(kPULL_RESP, R"({"txpk":{"modu":"FSK","datr":50000,"fdev":25000,"freq":868.3,"size":13,"data":"YAQDAgEAAQABAAAAAA=="}})");
  fwd.TestDnLocalData(fsk.data(), fsk.size(), peer);
  Expect(frame.ParseFromString(fwd.TestFirstFrame()), "FSK snapshot decodes");
  ExpectEq(frame.downlinks_size(), 1, "numeric FSK datr parses");
  Expect(frame.downlinks(0).dataratefsk() == 50000u, "FSK rate retained");
  Expect(frame.downlinks(0).fskfreqdev() == 25000.0f, "FSK deviation retained");
  Expect(frame.ParseFromString(fwd.TestTakeFirstFrame()), "clear capacity fixture");
  for (int i = 0; i < 4097; ++i) fwd.TestDnLocalData(ack.data(), ack.size(), peer);
  Expect(frame.ParseFromString(fwd.TestFirstFrame()), "capacity snapshot decodes");
  ExpectEq(frame.downlinkoutcomes_size(), 1, "full outcome batches flush before receiver capacity is exceeded");
  auto other = peer; other.port = htons(1701);
  fwd.TestDnLocalData(poll.data(), poll.size(), other);
  ExpectEq(fwd.TestFrameCount(), size_t{2}, "NAT peers isolated");
  return Summary();
}
