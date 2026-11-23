/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#pragma once

#include <cmath>
#include <cstdlib>
#include <iostream>
#include <string>

namespace bs_analytics_test {

inline int &Failures() {
  static int f = 0;
  return f;
}

inline void Expect(bool cond, const char *msg) {
  if (!cond) {
    std::cerr << "FAIL: " << msg << "\n";
    ++Failures();
  }
}

inline void ExpectEq(const std::string &got, const std::string &want,
                     const char *msg) {
  if (got != want) {
    std::cerr << "FAIL: " << msg << " got=[" << got << "] want=[" << want
              << "]\n";
    ++Failures();
  }
}

template <typename A, typename B>
inline void ExpectEq(A got, B want, const char *msg) {
  if (got != want) {
    std::cerr << "FAIL: " << msg << " got=" << got << " want=" << want << "\n";
    ++Failures();
  }
}

inline void ExpectNear(double got, double want, double eps, const char *msg) {
  if (std::fabs(got - want) > eps) {
    std::cerr << "FAIL: " << msg << " got=" << got << " want=" << want << "\n";
    ++Failures();
  }
}

inline int Summary() {
  if (Failures()) {
    std::cerr << Failures() << " failure(s)\n";
    return 1;
  }
  std::cout << "OK\n";
  return 0;
}

} // namespace bs_analytics_test
