#include "forwarder.h"
#include "dedupe.h"
#include "gateway_sync.h"
#include "logging.h"
#include "lora_mac.h"
#include "semtech_udp.h"
#include "Client.h"
#include "protobuf/analytics.pb.h"

#include <openssl/evp.h>
#include <chrono>
#include <cstdlib>
#include <cstring>
#include <ctime>
#include <thread>
#include <vector>

namespace forwarder_cc {

namespace {

api::LoRaCodingRate ParseCodingRate(const std::string& cr) {
  if (cr == "off") return api::CR_OFF;
  if (cr == "4/5") return api::CR_4_5;
  if (cr == "4/6") return api::CR_4_6;
  if (cr == "4/7") return api::CR_4_7;
  if (cr == "4/8") return api::CR_4_8;
  if (cr == "4/9") return api::CR_4_9;
  if (cr == "4/10") return api::CR_4_10;
  if (cr == "4/11") return api::CR_4_11;
  if (cr == "4/12") return api::CR_4_12;
  if (cr == "4/13") return api::CR_4_13;
  if (cr == "4/14") return api::CR_4_14;
  if (cr == "4/15") return api::CR_4_15;
  if (cr == "4/16") return api::CR_4_16;
  return api::CR_UNKNOWN;
}

api::CRCStatus ParseCrcStat(int stat) {
  if (stat == 1) return api::OK;
  if (stat == -1) return api::FAIL;
  return api::MISSING;
}

api::Modulation ParseModu(const std::string& modu) {
  if (modu == "LORA") return api::LORA;
  if (modu == "FSK") return api::FSK;
  return api::UNKNOWN;
}

api::LoRaSF ParseSF(const std::string& sf) {
  if (sf == "7") return api::SF7;
  if (sf == "8") return api::SF8;
  if (sf == "9") return api::SF9;
  if (sf == "10") return api::SF10;
  if (sf == "11") return api::SF11;
  if (sf == "12") return api::SF12;
  return api::SF_UNKNOWN;
}

api::LoRaBW ParseBW(const std::string& bw) {
  if (bw == "125") return api::BW_125k;
  if (bw == "250") return api::BW_250k;
  if (bw == "500") return api::BW_500k;
  return api::BW_UNKNOWN;
}

api::LoRaDataRate* ParseDataLoRaRate(const std::string& dataRate) {
  size_t bw = dataRate.find("BW");
  if (bw == std::string::npos || bw < 2) return nullptr;
  // Go: parseSF(dataRate[2:bw]) — strip leading "SF"
  api::LoRaDataRate* lora = new api::LoRaDataRate();
  std::string sf = dataRate.substr(2, bw - 2);
  std::string bwStr = dataRate.substr(bw + 2);
  lora->set_spreadingfactor(ParseSF(sf));
  lora->set_bandwidth(ParseBW(bwStr));
  return lora;
}

std::string Base64Decode(const std::string& in) {
  if (in.empty()) return "";
  std::string padded = in;
  while (padded.size() % 4) padded += '=';
  std::vector<unsigned char> out((padded.size() / 4) * 3 + 4);
  int len = EVP_DecodeBlock(out.data(), reinterpret_cast<const unsigned char*>(padded.data()),
                            static_cast<int>(padded.size()));
  if (len <= 0) return "";
  size_t pad = 0;
  if (in.size() >= 2 && in[in.size() - 1] == '=' && in[in.size() - 2] == '=') pad = 2;
  else if (in.size() >= 1 && in[in.size() - 1] == '=') pad = 1;
  size_t actual = static_cast<size_t>(len) - pad;
  if (actual > static_cast<size_t>(len)) actual = 0;
  return std::string(reinterpret_cast<char*>(out.data()), actual);
}

int64_t ParseTimeISO8601(const std::string& s) {
  if (s.empty()) return 0;
  struct tm t = {};
  int ms = 0;
  if (sscanf(s.c_str(), "%4d-%2d-%2dT%2d:%2d:%2d.%3dZ",
             &t.tm_year, &t.tm_mon, &t.tm_mday, &t.tm_hour, &t.tm_min, &t.tm_sec, &ms) >= 6) {
    t.tm_year -= 1900;
    t.tm_mon -= 1;
    time_t sec = timegm(&t);
    if (sec != static_cast<time_t>(-1)) return static_cast<int64_t>(sec) * 1000000 + ms * 1000;
  }
  return 0;
}

int64_t ParseTimeISO8601Millis(const std::string& s) {
  if (s.empty()) return 0;
  struct tm t = {};
  int ms = 0;
  if (sscanf(s.c_str(), "%4d-%2d-%2dT%2d:%2d:%2d.%3dZ",
             &t.tm_year, &t.tm_mon, &t.tm_mday, &t.tm_hour, &t.tm_min, &t.tm_sec, &ms) >= 6) {
    t.tm_year -= 1900;
    t.tm_mon -= 1;
    time_t sec = timegm(&t);
    if (sec != static_cast<time_t>(-1)) return static_cast<int64_t>(sec) * 1000 + ms;
  }
  return 0;
}

void IncPktStat(uint8_t kind, api::AnalyticsInternalMetrics* m) {
  if (!m) return;
  switch (kind) {
    case kPUSH_DATA: m->set_pktpush_data(m->pktpush_data() + 1); break;
    case kPUSH_ACK:  m->set_pktpush_ack(m->pktpush_ack() + 1); break;
    case kPULL_DATA: m->set_pktpull_data(m->pktpull_data() + 1); break;
    case kPULL_RESP: m->set_pktpull_resp(m->pktpull_resp() + 1); break;
    case kPULL_ACK:  m->set_pktpull_ack(m->pktpull_ack() + 1); break;
    case kTX_ACK:    m->set_pkttx_ack(m->pkttx_ack() + 1); break;
    default: break;
  }
}

void ResetFrameCounters(api::AnalyticsMetrics* frame) {
  frame->clear_uplinks();
  frame->clear_downlinks();
  frame->clear_stats();
  if (frame->has_metrics()) {
    auto* m = frame->mutable_metrics();
    m->set_uprxpackets(0);
    m->set_uptxpackets(0);
    m->set_dnrxpackets(0);
    m->set_dntxpackets(0);
    m->set_pktpush_data(0);
    m->set_pktpush_ack(0);
    m->set_pktpull_data(0);
    m->set_pktpull_ack(0);
    m->set_pktpull_resp(0);
    m->set_pkttx_ack(0);
  }
}

bool FrameHasData(const api::AnalyticsMetrics& f) {
  if (f.uplinks_size() || f.downlinks_size() || f.stats_size()) return true;
  if (f.has_metrics() && (f.metrics().uprxpackets() || f.metrics().uptxpackets() ||
                          f.metrics().dnrxpackets() || f.metrics().dntxpackets())) {
    return true;
  }
  return false;
}

std::string FrameKey(const Config& config, const SockAddr4& local_ep) {
  // Go keys by IP only for metrics-frame LRU.
  (void)config;
  return SockAddrIpString(local_ep);
}

}  // namespace

AnalyticsForwarder::AnalyticsForwarder(const Config& config,
                                       client_cc::Client* client,
                                       UdpProxy* proxy)
    : config_(config), client_(client), proxy_(proxy) {}

AnalyticsForwarder::~AnalyticsForwarder() {
  stopping_ = true;
  connected_ = false;
  if (gateway_sync_thread_.joinable()) gateway_sync_thread_.join();
}

api::AnalyticsMetrics* AnalyticsForwarder::GetOrCreateFrame(const SockAddr4& local_ep) {
  std::string key = FrameKey(config_, local_ep);
  std::vector<std::unique_ptr<api::AnalyticsMetrics>> to_flush;
  api::AnalyticsMetrics* raw = nullptr;

  {
    std::lock_guard<std::mutex> lock(metrics_mutex_);
    auto it = metrics_frames_.find(key);
    if (it != metrics_frames_.end()) {
      frame_order_.remove(key);
      frame_order_.push_back(key);
      return it->second.get();
    }

    while (static_cast<int>(metrics_frames_.size()) >= config_.max_udp_streams &&
           !frame_order_.empty()) {
      const std::string& oldest = frame_order_.front();
      auto eit = metrics_frames_.find(oldest);
      if (eit != metrics_frames_.end()) {
        to_flush.push_back(std::move(eit->second));
        metrics_frames_.erase(eit);
      }
      frame_order_.pop_front();
    }

    auto frame = std::make_unique<api::AnalyticsMetrics>();
    if (config_.server_side) {
      auto* m = frame->mutable_metrics();
      // Go sets GatewayIp to localEp.String() (IP:port) even though LRU key is IP-only.
      m->set_gatewayip(SockAddrToString(local_ep));
    }
    raw = frame.get();
    metrics_frames_[key] = std::move(frame);
    frame_order_.push_back(key);
  }

  // Flush evicted frames outside the metrics lock (copy+push; do not block UDP long).
  for (auto& f : to_flush) {
    if (f) FlushFrame(f.get());
  }
  return raw;
}

void AnalyticsForwarder::FlushFrame(api::AnalyticsMetrics* frame) {
  if (!frame || !FrameHasData(*frame)) return;
  api::AnalyticsMetrics copy;
  copy.CopyFrom(*frame);
  ResetFrameCounters(frame);
  if (!client_->PushMetrics(copy)) {
    LOG_WARN() << "Unable to push metrics";
  }
}

void AnalyticsForwarder::FlushAll() {
  sending_ = true;
  std::vector<api::AnalyticsMetrics> to_send;
  {
    std::lock_guard<std::mutex> lock(metrics_mutex_);
    for (auto& p : metrics_frames_) {
      api::AnalyticsMetrics* f = p.second.get();
      if (!FrameHasData(*f)) continue;
      to_send.emplace_back();
      to_send.back().CopyFrom(*f);
      ResetFrameCounters(f);
    }
  }
  for (auto& m : to_send) {
    if (!client_->PushMetrics(m)) {
      LOG_WARN() << "Unable to push metrics";
    }
  }
  sending_ = false;
}

bool AnalyticsForwarder::HasData() const {
  std::lock_guard<std::mutex> lock(metrics_mutex_);
  for (const auto& p : metrics_frames_) {
    if (FrameHasData(*p.second)) return true;
  }
  return false;
}

void AnalyticsForwarder::HandleUplink(const uint8_t* data, size_t len, api::AnalyticsMetrics* frame) {
  if (len < 4) return;
  SemtechUDPMessage msg;
  if (!DecodeSemtechUDP(data, len, msg)) return;
  IncPktStat(msg.kind, frame->has_metrics() ? frame->mutable_metrics() : nullptr);

  if (msg.kind != kPUSH_DATA) return;
  const uint8_t* eui = msg.GatewayEUI();
  if (!eui) return;

  frame->set_gatewayeui(eui, 8);
  if (!config_.server_side) frame->set_gatewayid(config_.gateway_id);

  SemtechJsonPayload payload;
  if (!ParseSemtechJsonPayload(msg, payload)) return;

  for (const auto& r : payload.rxpk) {
    api::AnalyticsUplink* up = frame->add_uplinks();
    up->set_rxfinishedtime(r.tmst);
    up->set_rxgpstime(r.tmms);
    up->set_frequency(r.frequency);
    up->set_rfchain(static_cast<uint32_t>(r.rfch));
    up->set_crc(ParseCrcStat(r.stat));
    up->set_modulation(ParseModu(r.modulation));
    up->set_codingrate(ParseCodingRate(r.codr));
    if (r.modulation == "LORA") {
      api::LoRaDataRate* lora = ParseDataLoRaRate(r.datr);
      if (lora) up->set_allocated_dataratelora(lora);
    } else if (r.modulation == "FSK") {
      int v = std::atoi(r.datr.c_str());
      up->set_dataratefsk(static_cast<uint32_t>(v));
    }
    up->set_rxwalltime(ParseTimeISO8601(r.time));
    if (!r.rsig.empty()) {
      for (const auto& ant : r.rsig) {
        auto* a = up->add_ant();
        a->set_antenna(ant.ant);
        a->set_ifchan(ant.chan);
        a->set_rssic(ant.rssic);
        a->set_lsnr(static_cast<float>(ant.lsnr));
        if (!ant.etime.empty()) a->set_etime(ant.etime);
        if (ant.rssis) a->set_rssis(*ant.rssis);
        if (ant.rssisd) a->set_rssisd(static_cast<int32_t>(*ant.rssisd));
        if (ant.ftime) a->set_ftime(*ant.ftime);
        if (ant.foff) a->set_foff(*ant.foff);
      }
    } else {
      auto* a = up->add_ant();
      a->set_antenna(0);
      a->set_ifchan(r.channel);
      a->set_rssic(static_cast<int32_t>(r.rssi));
      a->set_lsnr(r.lsnr);
    }
    up->set_size(static_cast<uint32_t>(r.size));
    std::string raw = Base64Decode(r.data);
    if (!raw.empty()) {
      int fhdr_len =
          GetLoRaWANHeaderLen(reinterpret_cast<const unsigned char*>(raw.data()), raw.size());
      up->set_fhdr(raw.data(), static_cast<size_t>(fhdr_len));
      ComputeUniqueIdUp(up, reinterpret_cast<const unsigned char*>(raw.data()), raw.size());
    }
  }
  if (payload.stat) {
    api::AnalyticsStat* st = frame->add_stats();
    st->set_gwtime(ParseTimeISO8601Millis(payload.stat->time));
    st->set_gwlatitude(payload.stat->lati);
    st->set_gwlongitude(payload.stat->long_);
    st->set_gwaltitude(payload.stat->alti);
    st->set_rxpackets(static_cast<uint32_t>(payload.stat->rxnb));
    st->set_rxwithvalidphycrc(static_cast<uint32_t>(payload.stat->rxok));
    st->set_rxforwarded(static_cast<uint32_t>(payload.stat->rxfw));
    st->set_rxackr(payload.stat->ackr);
    st->set_txreceived(static_cast<uint32_t>(payload.stat->dwnb));
    st->set_txemitted(static_cast<uint32_t>(payload.stat->txnb));
    st->set_isgauge(config_.gauge_stat);
  }
}

void AnalyticsForwarder::HandleDownlink(const uint8_t* data, size_t len,
                                        api::AnalyticsMetrics* frame) {
  if (len < 4) return;
  SemtechUDPMessage msg;
  if (!DecodeSemtechUDP(data, len, msg)) return;
  IncPktStat(msg.kind, frame->has_metrics() ? frame->mutable_metrics() : nullptr);

  const uint8_t* eui = msg.GatewayEUI();
  if (eui) frame->set_gatewayeui(eui, 8);

  if (msg.kind != kPULL_RESP) return;
  SemtechJsonPayload payload;
  if (!ParseSemtechJsonPayload(msg, payload) || !payload.txpk) return;

  const auto& t = *payload.txpk;
  api::AnalyticsDownlink* down = frame->add_downlinks();
  down->set_txtime(t.tmst);
  down->set_txgpstime(t.tmms);
  down->set_fskfreqdev(t.fdev);
  down->set_frequency(t.frequency);
  down->set_rfchain(static_cast<uint32_t>(t.rfch));
  down->set_power(t.powe);
  down->set_modulation(ParseModu(t.modu));
  down->set_codingrate(ParseCodingRate(t.codr));
  if (t.modu == "LORA") {
    api::LoRaDataRate* lora = ParseDataLoRaRate(t.datr);
    if (lora) down->set_allocated_dataratelora(lora);
  } else if (t.modu == "FSK") {
    int v = std::atoi(t.datr.c_str());
    down->set_dataratefsk(static_cast<uint32_t>(v));
  }
  down->set_invertpolarity(t.ipol);
  down->set_immediately(t.imme);
  down->set_rfpreamble(static_cast<uint32_t>(t.prea));
  down->set_size(static_cast<uint32_t>(t.size));
  down->set_nocrc(t.ncrc);
  down->set_rxwalltime(std::chrono::duration_cast<std::chrono::milliseconds>(
                           std::chrono::system_clock::now().time_since_epoch())
                           .count());
  std::string raw = Base64Decode(t.data);
  if (!raw.empty()) {
    int fhdr_len =
        GetLoRaWANHeaderLen(reinterpret_cast<const unsigned char*>(raw.data()), raw.size());
    down->set_fhdr(raw.data(), static_cast<size_t>(fhdr_len));
    ComputeUniqueIdDown(down, reinterpret_cast<const unsigned char*>(raw.data()), raw.size());
  }
}

void AnalyticsForwarder::UpLocalData(const uint8_t* data, size_t len, const SockAddr4& local_ep) {
  api::AnalyticsMetrics* frame = GetOrCreateFrame(local_ep);
  if (!frame) return;
  if (frame->has_metrics()) frame->mutable_metrics()->set_uptxpackets(frame->metrics().uptxpackets() + 1);
  if (len < 4) return;
  if (SemtechUDPIsUplink(data)) HandleUplink(data, len, frame);
  else if (SemtechUDPIsDownlink(data)) HandleDownlink(data, len, frame);
}

void AnalyticsForwarder::UpRemoteData(const uint8_t* data, size_t len, const SockAddr4& local_ep) {
  api::AnalyticsMetrics* frame = GetOrCreateFrame(local_ep);
  if (!frame) return;
  if (frame->has_metrics()) frame->mutable_metrics()->set_uprxpackets(frame->metrics().uprxpackets() + 1);
  if (len < 4) return;
  if (SemtechUDPIsUplink(data)) HandleUplink(data, len, frame);
  else if (SemtechUDPIsDownlink(data)) HandleDownlink(data, len, frame);
}

void AnalyticsForwarder::DnLocalData(const uint8_t* data, size_t len, const SockAddr4& local_ep) {
  api::AnalyticsMetrics* frame = GetOrCreateFrame(local_ep);
  if (!frame) return;
  if (frame->has_metrics()) frame->mutable_metrics()->set_dntxpackets(frame->metrics().dntxpackets() + 1);
  if (len < 4) return;
  if (SemtechUDPIsUplink(data)) HandleUplink(data, len, frame);
  else if (SemtechUDPIsDownlink(data)) HandleDownlink(data, len, frame);
}

void AnalyticsForwarder::DnRemoteData(const uint8_t* data, size_t len, const SockAddr4& local_ep) {
  api::AnalyticsMetrics* frame = GetOrCreateFrame(local_ep);
  if (!frame) return;
  if (frame->has_metrics()) frame->mutable_metrics()->set_dnrxpackets(frame->metrics().dnrxpackets() + 1);
  if (len < 4) return;
  if (SemtechUDPIsUplink(data)) HandleUplink(data, len, frame);
  else if (SemtechUDPIsDownlink(data)) HandleDownlink(data, len, frame);
}

void AnalyticsForwarder::ConnectLoop() {
  while (!connected_ && !stopping_) {
    LOG_DEBUG() << "Connecting to: " << config_.endpoint;
    if (client_->Connect()) {
      connected_ = true;
      return;
    }
    LOG_WARN() << "Could not connect to analytics endpoint: " << client_->LastError();
    for (int i = 0; i < 100 && !stopping_; ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
  }
}

void AnalyticsForwarder::FlushLoop() {
  while (connected_ && !stopping_) {
    for (int i = 0; i < config_.flush_interval * 10 && connected_ && !stopping_; ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    if (!sending_ && connected_ && !stopping_ && HasData()) FlushAll();
  }
}

void AnalyticsForwarder::ScheduleGatewayUpsert() {
  ParsedGatewaySync parsed;
  std::string err;
  bool ok = ParseGatewaySyncConfig(config_, parsed, err);
  if (!ok) {
    if (!err.empty()) LOG_WARN() << "Skipping gateway upsert: " << err;
    return;
  }
  if (!parsed.has_any) return;

  api::ReqGatewayUpsert req = ToUpsertRequest(parsed);
  if (gateway_sync_thread_.joinable()) gateway_sync_thread_.join();
  gateway_sync_thread_ = std::thread([this, req = std::move(req)]() mutable {
    api::RespGatewaySync resp;
    if (!client_->GatewayUpsert(req, &resp)) {
      LOG_WARN() << "Gateway upsert failed";
      return;
    }
    if (resp.applied()) {
      LOG_INFO() << "Gateway upsert applied (id=" << resp.gateway_id() << ")";
    } else {
      LOG_INFO() << "Gateway upsert acknowledged without mutation (id=" << resp.gateway_id()
                 << ")";
    }
  });
}

size_t AnalyticsForwarder::TestFrameCount() const {
  std::lock_guard<std::mutex> lock(metrics_mutex_);
  return metrics_frames_.size();
}

int AnalyticsForwarder::TestUplinkCount() const {
  std::lock_guard<std::mutex> lock(metrics_mutex_);
  int n = 0;
  for (const auto& p : metrics_frames_) n += p.second->uplinks_size();
  return n;
}

int AnalyticsForwarder::TestDownlinkCount() const {
  std::lock_guard<std::mutex> lock(metrics_mutex_);
  int n = 0;
  for (const auto& p : metrics_frames_) n += p.second->downlinks_size();
  return n;
}

api::LoRaSF AnalyticsForwarder::TestFirstUplinkSF() const {
  std::lock_guard<std::mutex> lock(metrics_mutex_);
  for (const auto& p : metrics_frames_) {
    if (p.second->uplinks_size() > 0 && p.second->uplinks(0).has_dataratelora()) {
      return p.second->uplinks(0).dataratelora().spreadingfactor();
    }
  }
  return api::SF_UNKNOWN;
}

std::string AnalyticsForwarder::TestFirstAntennaETime() const {
  std::lock_guard<std::mutex> lock(metrics_mutex_);
  for (const auto& p : metrics_frames_) {
    // Prefer the most recently added uplink that has etime.
    for (int i = p.second->uplinks_size() - 1; i >= 0; --i) {
      if (p.second->uplinks(i).ant_size() > 0 && !p.second->uplinks(i).ant(0).etime().empty()) {
        return p.second->uplinks(i).ant(0).etime();
      }
    }
  }
  return "";
}

void AnalyticsForwarder::Run() {
  // Install proxy event handlers first so gateway sync / connect never block
  // packet forwarding. Proxy threads are started by the caller via Start()+Run,
  // or we drive connect in the background while proxying.
  UdpProxyEvents ev;
  ev.up_local_data = [this](const uint8_t* d, size_t n, const SockAddr4& ep) { UpLocalData(d, n, ep); };
  ev.up_remote_data = [this](const uint8_t* d, size_t n, const SockAddr4& ep) { UpRemoteData(d, n, ep); };
  ev.dn_local_data = [this](const uint8_t* d, size_t n, const SockAddr4& ep) { DnLocalData(d, n, ep); };
  ev.dn_remote_data = [this](const uint8_t* d, size_t n, const SockAddr4& ep) { DnRemoteData(d, n, ep); };
  proxy_->SetEvents(std::move(ev));

  // Connect in background; proxying must not wait on analytics availability.
  std::thread connect_thread([this]() {
    ConnectLoop();
    if (!connected_) return;
    LOG_INFO() << "Connected to kudzu analytics";
    ScheduleGatewayUpsert();
    FlushLoop();
  });

  proxy_->Run();
  stopping_ = true;
  connected_ = false;
  if (connect_thread.joinable()) connect_thread.join();
  if (gateway_sync_thread_.joinable()) gateway_sync_thread_.join();
}

}  // namespace forwarder_cc
