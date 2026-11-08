/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#pragma once

#include <cstddef>
#include <cstdint>
#include <deque>
#include <mutex>
#include <utility>

#include "protobuf/analytics.pb.h"

namespace basicstation_analytics {

class BatchQueue {
public:
  explicit BatchQueue(size_t max_batches = 32) : max_batches_(max_batches ? max_batches : 32) {}

  // Enqueue a copy. If full, drop oldest and return true (dropped).
  bool Push(const api::AnalyticsMetrics& batch) {
    std::lock_guard<std::mutex> lock(mu_);
    bool dropped = false;
    while (q_.size() >= max_batches_) {
      q_.pop_front();
      ++drops_;
      dropped = true;
    }
    q_.push_back(batch);
    return dropped;
  }

  bool Pop(api::AnalyticsMetrics* out) {
    std::lock_guard<std::mutex> lock(mu_);
    if (q_.empty() || !out) return false;
    *out = std::move(q_.front());
    q_.pop_front();
    return true;
  }

  // Put a batch back at the front (failed send).
  void PushFront(api::AnalyticsMetrics batch) {
    std::lock_guard<std::mutex> lock(mu_);
    q_.push_front(std::move(batch));
    while (q_.size() > max_batches_) {
      q_.pop_back();
      ++drops_;
    }
  }

  size_t Size() const {
    std::lock_guard<std::mutex> lock(mu_);
    return q_.size();
  }

  uint64_t Drops() const {
    std::lock_guard<std::mutex> lock(mu_);
    return drops_;
  }

private:
  size_t max_batches_;
  mutable std::mutex mu_;
  std::deque<api::AnalyticsMetrics> q_;
  uint64_t drops_ = 0;
};

}  // namespace basicstation_analytics
