#include "semtech_udp.h"
#include <cstring>

namespace forwarder_cc {

bool DecodeSemtechUDP(const uint8_t* payload, size_t size, SemtechUDPMessage& msg) {
  if (size < 4) return false;
  if (payload[0] != kSemtechProtocolVersion) return false;

  msg.version = payload[0];
  msg.token = static_cast<uint16_t>(payload[1]) | (static_cast<uint16_t>(payload[2]) << 8);
  msg.kind = payload[3];
  if (size > 4) {
    msg.data.assign(reinterpret_cast<const char*>(payload + 4), size - 4);
  } else {
    msg.data.clear();
  }
  return true;
}

static void FromJson(const nlohmann::json& j, SemtechRxPktRsig& r) {
  r.ant = j.value("ant", 0);
  r.chan = j.value("chan", 0);
  r.rssic = j.value("rssic", 0);
  if (j.contains("rssis")) r.rssis = j["rssis"].get<int>();
  if (j.contains("rssisd")) r.rssisd = j["rssisd"].get<uint16_t>();
  r.lsnr = j.value("lsnr", 0.0);
  if (j.contains("etime") && j["etime"].is_string()) r.etime = j["etime"].get<std::string>();
  if (j.contains("ftime")) r.ftime = j["ftime"].get<int64_t>();
  if (j.contains("foff")) r.foff = j["foff"].get<int32_t>();
}

static void FromJson(const nlohmann::json& j, SemtechRxPkt& r) {
  if (j.contains("time")) r.time = j["time"].get<std::string>();
  r.tmms = j.value("tmms", static_cast<int64_t>(0));
  r.tmst = j.value("tmst", static_cast<int64_t>(0));
  r.frequency = j.value("freq", 0.f);
  r.channel = j.value("chan", 0);
  r.rfch = j.value("rfch", 0);
  r.stat = j.value("stat", 0);
  if (j.contains("modu")) r.modulation = j["modu"].get<std::string>();
  if (j.contains("datr")) r.datr = j["datr"].get<std::string>();
  if (j.contains("codr")) r.codr = j["codr"].get<std::string>();
  r.rssi = j.value("rssi", 0.f);
  r.lsnr = j.value("lsnr", 0.f);
  r.size = j.value("size", 0);
  if (j.contains("data")) r.data = j["data"].get<std::string>();
  if (j.contains("rsig") && j["rsig"].is_array()) {
    for (const auto& e : j["rsig"]) {
      SemtechRxPktRsig rs;
      FromJson(e, rs);
      r.rsig.push_back(rs);
    }
  }
}

static void FromJson(const nlohmann::json& j, SemtechTxPkt& t) {
  t.imme = j.value("imme", false);
  t.tmst = j.value("tmst", static_cast<int64_t>(0));
  t.tmms = j.value("tmms", static_cast<int64_t>(0));
  t.frequency = j.value("freq", 0.f);
  t.rfch = j.value("rfch", 0);
  t.powe = j.value("powe", 0.f);
  if (j.contains("modu")) t.modu = j["modu"].get<std::string>();
  if (j.contains("datr")) t.datr = j["datr"].get<std::string>();
  if (j.contains("codr")) t.codr = j["codr"].get<std::string>();
  t.fdev = j.value("fdev", 0.f);
  t.ipol = j.value("ipol", false);
  t.prea = j.value("prea", 0);
  t.ncrc = j.value("ncrc", false);
  t.size = j.value("size", 0);
  if (j.contains("data")) t.data = j["data"].get<std::string>();
}

static void FromJson(const nlohmann::json& j, SemtechStat& s) {
  if (j.contains("time")) s.time = j["time"].get<std::string>();
  s.lati = j.value("lati", 0.f);
  s.long_ = j.value("long", 0.f);  // JSON key is "long"
  s.alti = j.value("alti", 0.f);
  s.rxnb = j.value("rxnb", 0);
  s.rxok = j.value("rxok", 0);
  s.rxfw = j.value("rxfw", 0);
  s.ackr = j.value("ackr", 0.f);
  s.dwnb = j.value("dwnb", 0);
  s.txnb = j.value("txnb", 0);
}

bool ParseSemtechJsonPayload(const SemtechUDPMessage& msg, SemtechJsonPayload& out) {
  const char* body = msg.BodyPtr();
  size_t body_len = msg.BodySize();
  if (!body || body_len == 0) return false;

  nlohmann::json j;
  try {
    j = nlohmann::json::parse(body, body + body_len);
  } catch (...) {
    return false;
  }

  if (j.contains("rxpk") && j["rxpk"].is_array()) {
    for (const auto& e : j["rxpk"]) {
      SemtechRxPkt r;
      FromJson(e, r);
      out.rxpk.push_back(r);
    }
  }
  if (j.contains("txpk") && j["txpk"].is_object()) {
    SemtechTxPkt t;
    FromJson(j["txpk"], t);
    out.txpk = t;
  }
  if (j.contains("stat") && j["stat"].is_object()) {
    SemtechStat s;
    FromJson(j["stat"], s);
    out.stat = s;
  }
  return true;
}

}  // namespace forwarder_cc
