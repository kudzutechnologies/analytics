#pragma once

#include "config.h"
#include "udp_proxy.h"

#include <atomic>
#include <list>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <unordered_map>

namespace client_cc {
class Client;
}

namespace api {
class AnalyticsMetrics;
enum LoRaSF : int;
}

namespace forwarder_cc {

class AnalyticsForwarder {
 public:
  AnalyticsForwarder(const Config& config,
                     client_cc::Client* client,
                     UdpProxy* proxy);
  ~AnalyticsForwarder();

  void Run();

  // Lightweight inspection hooks for unit tests.
  size_t TestFrameCount() const;
  int TestUplinkCount() const;
  int TestDownlinkCount() const;
  api::LoRaSF TestFirstUplinkSF() const;
  std::string TestFirstAntennaETime() const;
  void TestUpLocalData(const uint8_t* data, size_t len, const SockAddr4& local_ep) {
    UpLocalData(data, len, local_ep);
  }
  void TestDnLocalData(const uint8_t* data, size_t len, const SockAddr4& local_ep) {
    DnLocalData(data, len, local_ep);
  }

 private:
  api::AnalyticsMetrics* GetOrCreateFrame(const SockAddr4& local_ep);
  void FlushFrame(api::AnalyticsMetrics* frame);
  void FlushAll();
  bool HasData() const;
  void HandleUplink(const uint8_t* data, size_t len, api::AnalyticsMetrics* frame);
  void HandleDownlink(const uint8_t* data, size_t len, api::AnalyticsMetrics* frame);
  void UpLocalData(const uint8_t* data, size_t len, const SockAddr4& local_ep);
  void UpRemoteData(const uint8_t* data, size_t len, const SockAddr4& local_ep);
  void DnLocalData(const uint8_t* data, size_t len, const SockAddr4& local_ep);
  void DnRemoteData(const uint8_t* data, size_t len, const SockAddr4& local_ep);
  void ConnectLoop();
  void FlushLoop();
  void ScheduleGatewayUpsert();

  Config config_;
  client_cc::Client* client_ = nullptr;
  UdpProxy* proxy_ = nullptr;
  mutable std::mutex metrics_mutex_;
  // Recency-ordered keys (front = oldest) + frames keyed by IP (server-side) or IP:port.
  std::list<std::string> frame_order_;
  std::unordered_map<std::string, std::unique_ptr<api::AnalyticsMetrics>> metrics_frames_;
  std::atomic<bool> connected_{false};
  std::atomic<bool> sending_{false};
  std::atomic<bool> stopping_{false};
  std::thread gateway_sync_thread_;
};

}  // namespace forwarder_cc
