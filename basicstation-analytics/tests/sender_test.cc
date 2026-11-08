#include "sender.h"
#include "tests/test_harness.h"
#include <condition_variable>
#include <mutex>
#include <thread>
using namespace basicstation_analytics;
using namespace bs_analytics_test;

class BlockedTransport : public AnalyticsTransport {
public:
  grpc::Status Connect(client_cc::Client::Deadline) override {
    return grpc::Status::OK;
  }
  grpc::Status Push(const api::AnalyticsMetrics &,
                    client_cc::Client::Deadline) override {
    std::unique_lock<std::mutex> lock(mu);
    entered = true;
    cv.notify_all();
    cv.wait(lock, [&] { return cancelled; });
    return {grpc::StatusCode::CANCELLED, "shutdown"};
  }
  void Disconnect() override {}
  void Cancel() override {
    std::lock_guard<std::mutex> lock(mu);
    cancelled = true;
    cv.notify_all();
  }
  bool Wait() {
    std::unique_lock<std::mutex> lock(mu);
    return cv.wait_for(lock, std::chrono::seconds(2), [&] { return entered; });
  }

private:
  std::mutex mu;
  std::condition_variable cv;
  bool entered = false, cancelled = false;
};

int main() {
  BatchQueue q(1, 1, 1024);
  api::AnalyticsMetrics m;
  m.add_uplinks()->set_size(4);
  Expect(!q.Push(m), "first batch admitted");
  auto item = q.Next();
  Expect(bool(item), "full batch ready");
  Expect(q.Push(m), "in-flight capacity rejects incoming record");
  ExpectEq(q.Size(), size_t(1), "in-flight counted");
  q.Complete(item, false);
  Expect(q.Next() == item, "failed payload retained");
  q.Complete(item, true);
  ExpectEq(q.Size(), size_t(0), "acknowledged batch released");
  for (int i = 0; i < 10000; ++i) {
    q.Push(m);
    Expect(q.Bytes() <= 1024, "byte cap");
    Expect(q.Size() <= 1, "batch cap");
  }
  BlockedTransport transport;
  Sender sender(&transport, &q);
  sender.Start();
  Expect(transport.Wait(), "backend entered push");
  auto before = std::chrono::steady_clock::now();
  Expect(!sender.Shutdown(100), "cancelled push is not reported drained");
  auto elapsed = std::chrono::steady_clock::now() - before;
  Expect(elapsed < std::chrono::milliseconds(350),
         "shutdown obeys deadline allowance");
  BatchQueue pending(1, 1, 1024, true, 0);
  pending.Push(m);
  ExpectEq(pending.Size(), size_t(0), "zero pre-identity retention");
  return Summary();
}
