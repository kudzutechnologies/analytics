/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "sender.h"
#include "logging.h"

#include <chrono>
#include <thread>

namespace basicstation_analytics {

void Sender::Start() {
  if (running_) return;
  running_ = true;
  thr_ = std::thread([this] { Loop(); });
}

void Sender::Stop() {
  running_ = false;
  if (thr_.joinable()) thr_.join();
}

bool Sender::EnsureConnected() {
  if (connected_) return true;
  if (client_->Connect()) {
    connected_ = true;
    backoff_sec_ = 1;
    return true;
  }
  ++reconnects_;
  LOG_WARN() << "analytics connect failed: " << client_->LastError();
  std::this_thread::sleep_for(std::chrono::seconds(backoff_sec_));
  backoff_sec_ = std::min(backoff_sec_ * 2, 60);
  return false;
}

bool Sender::PushOne(api::AnalyticsMetrics& batch) {
  if (!client_->PushMetrics(batch)) {
    ++push_failures_;
    LOG_WARN() << "PushMetrics failed: " << client_->LastError();
    client_->Disconnect();
    connected_ = false;
    return false;
  }
  return true;
}

void Sender::Loop() {
  while (running_) {
    api::AnalyticsMetrics batch;
    if (!queue_->Pop(&batch)) {
      std::this_thread::sleep_for(std::chrono::milliseconds(50));
      continue;
    }
    if (!EnsureConnected() || !PushOne(batch)) {
      queue_->PushFront(std::move(batch));
      std::this_thread::sleep_for(std::chrono::seconds(backoff_sec_));
      backoff_sec_ = std::min(backoff_sec_ * 2, 60);
    } else {
      backoff_sec_ = 1;
    }
  }
}

bool Sender::Drain(int deadline_ms) {
  auto deadline = std::chrono::steady_clock::now() + std::chrono::milliseconds(deadline_ms);
  while (queue_->Size() > 0) {
    if (std::chrono::steady_clock::now() >= deadline) return false;
    if (!EnsureConnected()) continue;
    api::AnalyticsMetrics batch;
    if (!queue_->Pop(&batch)) break;
    if (!PushOne(batch)) {
      queue_->PushFront(std::move(batch));
      return false;
    }
  }
  return queue_->Size() == 0;
}

}  // namespace basicstation_analytics
