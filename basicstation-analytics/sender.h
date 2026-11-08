/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#pragma once

#include <atomic>
#include <string>
#include <thread>

#include "batch_queue.h"
#include "Client.h"

namespace basicstation_analytics {

class Sender {
public:
  Sender(client_cc::Client* client, BatchQueue* queue)
      : client_(client), queue_(queue) {}
  ~Sender() { Stop(); }

  void Start();
  void Stop();
  // Drain with deadline_ms; return false if batches remain.
  bool Drain(int deadline_ms);

  uint64_t PushFailures() const { return push_failures_; }
  uint64_t Reconnects() const { return reconnects_; }

private:
  void Loop();
  bool EnsureConnected();
  bool PushOne(api::AnalyticsMetrics& batch);

  client_cc::Client* client_;
  BatchQueue* queue_;
  std::atomic<bool> running_{false};
  std::thread thr_;
  std::atomic<uint64_t> push_failures_{0};
  std::atomic<uint64_t> reconnects_{0};
  int backoff_sec_ = 1;
  bool connected_ = false;
};

}  // namespace basicstation_analytics
