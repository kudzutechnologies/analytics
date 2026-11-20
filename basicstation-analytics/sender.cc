/* SPDX-License-Identifier: MIT */
#include "sender.h"
#include "logging.h"
namespace basicstation_analytics {
void Sender::Start() {
  std::lock_guard<std::mutex> lock(mu_);
  if (thread_.joinable())
    return;
  running_ = true;
  done_ = false;
  thread_ = std::thread([this] { Loop(); });
}
bool Sender::Backoff(int seconds) {
  std::unique_lock<std::mutex> lock(mu_);
  cv_.wait_for(lock, std::chrono::seconds(seconds),
               [this] { return !running_; });
  return running_;
}
void Sender::Loop() {
  bool connected = false;
  int backoff = 1;
  while (running_ && !queue_->Finished()) {
    if (!connected) {
      auto status = transport_->Connect(std::chrono::system_clock::now() +
                                        std::chrono::seconds(connect_sec_));
      if (!status.ok()) {
        ++reconnects_;
        if (running_)
          LOG_WARN() << "analytics connection failed status="
                     << status.error_code() << " retry_seconds=" << backoff;
        if (!Backoff(backoff))
          break;
        backoff = (backoff > max_backoff_ / 2 ? max_backoff_ : backoff * 2);
        continue;
      }
      connected = true;
      if (on_connected_)
        on_connected_();
    }
    auto item = queue_->Next();
    if (!item) {
      queue_->Wait(std::chrono::milliseconds(100));
      continue;
    }
    auto status =
        transport_->Push(item->metrics, std::chrono::system_clock::now() +
                                            std::chrono::seconds(request_sec_));
    queue_->Complete(item, status.ok(),
                     status.error_code() == grpc::StatusCode::INVALID_ARGUMENT);
    if (!status.ok()) {
      ++failures_;
      if (running_)
        LOG_WARN() << "analytics push failed status=" << status.error_code()
                   << " retry_seconds=" << backoff;
      transport_->Disconnect();
      connected = false;
      if (!Backoff(backoff))
        break;
      backoff = (backoff > max_backoff_ / 2 ? max_backoff_ : backoff * 2);
    } else
      backoff = 1;
  }
  transport_->Disconnect();
  {
    std::lock_guard<std::mutex> lock(mu_);
    done_ = true;
  }
  cv_.notify_all();
}
bool Sender::Shutdown(int deadline_ms) {
  queue_->Seal(true);
  if (!thread_.joinable())
    return queue_->Size() == 0;
  auto deadline = std::chrono::steady_clock::now() +
                  std::chrono::milliseconds(std::max(0, deadline_ms));
  {
    std::unique_lock<std::mutex> lock(mu_);
    cv_.wait_until(lock, deadline, [this] { return done_; });
  }
  running_ = false;
  transport_->Cancel();
  cv_.notify_all();
  queue_->Wake();
  thread_.join();
  return queue_->Size() == 0;
}
} // namespace basicstation_analytics
