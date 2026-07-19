#pragma once

#include <cstdint>
#include <memory>
#include <optional>
#include <string>
#include <vector>
#include <nlohmann/json.hpp>

namespace forwarder_cc {

constexpr uint8_t kSemtechProtocolVersion = 2;
constexpr uint8_t kPUSH_DATA = 0x00;
constexpr uint8_t kPUSH_ACK  = 0x01;
constexpr uint8_t kPULL_DATA = 0x02;
constexpr uint8_t kPULL_RESP = 0x03;
constexpr uint8_t kPULL_ACK  = 0x04;
constexpr uint8_t kTX_ACK    = 0x05;

inline bool SemtechUDPIsUplink(const uint8_t* b) {
  if (b[0] != kSemtechProtocolVersion) return false;
  switch (b[3]) {
    case kPUSH_DATA:
    case kPUSH_ACK:
      return true;
    default:
      return false;
  }
}

inline bool SemtechUDPIsDownlink(const uint8_t* b) {
  if (b[0] != kSemtechProtocolVersion) return false;
  switch (b[3]) {
    case kPULL_DATA:
    case kPULL_RESP:
    case kPULL_ACK:
    case kTX_ACK:
      return true;
    default:
      return false;
  }
}

struct SemtechUDPMessage {
  uint8_t version = 0;
  uint16_t token = 0;
  uint8_t kind = 0;
  std::string data;  // payload after header (for PUSH_DATA/PULL_DATA/TX_ACK: includes 8-byte gateway EUI)

  const uint8_t* GatewayEUI() const {
    if (kind == kPUSH_DATA || kind == kPULL_DATA || kind == kTX_ACK) {
      if (data.size() >= 8) return reinterpret_cast<const uint8_t*>(data.data());
    }
    return nullptr;
  }

  const char* BodyPtr() const {
    if (kind == kPUSH_DATA && data.size() > 8) return data.data() + 8;
    if (kind == kPULL_RESP) return data.data();
    if (kind == kTX_ACK && data.size() > 8) return data.data() + 8;
    return nullptr;
  }

  size_t BodySize() const {
    if (kind == kPUSH_DATA && data.size() > 8) return data.size() - 8;
    if (kind == kPULL_RESP) return data.size();
    if (kind == kTX_ACK && data.size() > 8) return data.size() - 8;
    return 0;
  }
};

// Returns true and fills msg if packet is valid (at least 4 bytes, correct version).
bool DecodeSemtechUDP(const uint8_t* payload, size_t size, SemtechUDPMessage& msg);

// JSON payload structures matching Semtech UDP
struct SemtechRxPktRsig {
  int ant = 0;
  int chan = 0;
  int rssic = 0;
  std::optional<int> rssis;
  std::optional<uint16_t> rssisd;
  double lsnr = 0;
  std::string etime;
  std::optional<int64_t> ftime;
  std::optional<int32_t> foff;
};

struct SemtechRxPkt {
  std::string time;
  int64_t tmms = 0;
  int64_t tmst = 0;
  float frequency = 0;
  int channel = 0;
  int rfch = 0;
  int stat = 0;
  std::string modulation;
  std::string datr;
  std::string codr;
  float rssi = 0;
  float lsnr = 0;
  int size = 0;
  std::string data;  // base64
  std::vector<SemtechRxPktRsig> rsig;
};

struct SemtechTxPkt {
  bool imme = false;
  int64_t tmst = 0;
  int64_t tmms = 0;
  float frequency = 0;
  int rfch = 0;
  float powe = 0;
  std::string modu;
  std::string datr;
  std::string codr;
  float fdev = 0;
  bool ipol = false;
  int prea = 0;
  bool ncrc = false;
  int size = 0;
  std::string data;  // base64
};

struct SemtechStat {
  std::string time;
  float lati = 0;
  float long_ = 0;
  float alti = 0;
  int rxnb = 0;
  int rxok = 0;
  int rxfw = 0;
  float ackr = 0;
  int dwnb = 0;
  int txnb = 0;
};

struct SemtechJsonPayload {
  std::vector<SemtechRxPkt> rxpk;
  std::optional<SemtechTxPkt> txpk;
  std::optional<SemtechStat> stat;
};

bool ParseSemtechJsonPayload(const SemtechUDPMessage& msg, SemtechJsonPayload& out);

}  // namespace forwarder_cc
