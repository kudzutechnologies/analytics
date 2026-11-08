/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "batch_queue.h"
#include "tests/test_harness.h"
#include "protobuf/analytics.pb.h"

using namespace bs_analytics_test;
using namespace basicstation_analytics;

int main() {
  BatchQueue q(2);
  api::AnalyticsMetrics a, b, c, out;
  a.add_uplinks();
  b.add_uplinks();
  b.add_uplinks();
  c.add_downlinks();

  Expect(!q.Push(a), "no drop");
  Expect(!q.Push(b), "no drop");
  Expect(q.Push(c), "drop oldest");
  ExpectEq(q.Drops(), uint64_t{1}, "one drop");
  Expect(q.Pop(&out), "pop");
  ExpectEq(out.uplinks_size(), 2, "got b");
  Expect(q.Pop(&out), "pop2");
  ExpectEq(out.downlinks_size(), 1, "got c");
  Expect(!q.Pop(&out), "empty");
  return Summary();
}
