/* SPDX-License-Identifier: MIT */
#pragma once
#include "Client.h"
#include "batch_queue.h"
#include <atomic>
#include <chrono>
#include <condition_variable>
#include <functional>
#include <mutex>
#include <thread>

namespace basicstation_analytics {
class AnalyticsTransport {
public:
  virtual ~AnalyticsTransport() = default;
  virtual grpc::Status Connect(client_cc::Client::Deadline) = 0;
  virtual grpc::Status Push(const api::AnalyticsMetrics &,
                            client_cc::Client::Deadline) = 0;
  virtual void Disconnect() = 0;
  virtual void Cancel() = 0;
};
class ClientTransport final : public AnalyticsTransport {
public:
  explicit ClientTransport(client_cc::Client *c) : client_(c) {}
  grpc::Status Connect(client_cc::Client::Deadline d) override {
    return client_->ConnectOnce(d);
  }
  grpc::Status Push(const api::AnalyticsMetrics &m,
                    client_cc::Client::Deadline d) override {
    return client_->PushMetricsOnce(m, d);
  }
  void Disconnect() override { client_->Disconnect(); }
  void Cancel() override { client_->CancelPending(); }

private:
  client_cc::Client *client_;
};
class Sender {
public:
  Sender(client_cc::Client *c, BatchQueue *q, int connect_sec = 5,
         int request_sec = 5, int max_backoff = 60,
         std::function<void()> on_connected = {})
      : adapter_(c), transport_(&adapter_), queue_(q),
        connect_sec_(connect_sec), request_sec_(request_sec),
        max_backoff_(max_backoff), on_connected_(std::move(on_connected)) {}
  Sender(AnalyticsTransport *t, BatchQueue *q)
      : adapter_(nullptr), transport_(t), queue_(q) {}
  ~Sender() { Stop(); }
  void Start();
  void Stop() { Shutdown(0); }
  bool Drain(int deadline_ms) { return Shutdown(deadline_ms); }
  bool Shutdown(int deadline_ms);
  uint64_t PushFailures() const { return failures_; }
  uint64_t Reconnects() const { return reconnects_; }

private:
  void Loop();
  bool Backoff(int seconds);
  ClientTransport adapter_;
  AnalyticsTransport *transport_;
  BatchQueue *queue_;
  int connect_sec_ = 5, request_sec_ = 5, max_backoff_ = 60;
  std::function<void()> on_connected_;
  std::atomic<bool> running_{false};
  std::thread thread_;
  std::atomic<uint64_t> failures_{0}, reconnects_{0};
  std::mutex mu_;
  std::condition_variable cv_;
  bool done_ = true;
};
} // namespace basicstation_analytics
