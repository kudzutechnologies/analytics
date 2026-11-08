/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#pragma once

#include <ostream>
#include <sstream>
#include <string>

namespace basicstation_analytics {

enum class LogLevel { kDebug = 0, kInfo = 1, kWarn = 2, kError = 3 };

void InitLogging(const std::string& level_name, const std::string& log_file = "");
void SetLogLevel(LogLevel level);
LogLevel ParseLogLevel(const std::string& name);

class LogMessage {
 public:
  explicit LogMessage(LogLevel level);
  ~LogMessage();
  LogMessage(const LogMessage&) = delete;
  LogMessage& operator=(const LogMessage&) = delete;
  std::ostream& stream();

 private:
  LogLevel level_;
  bool enabled_;
  std::ostringstream buf_;
};

#define LOG_DEBUG() (::basicstation_analytics::LogMessage(::basicstation_analytics::LogLevel::kDebug).stream())
#define LOG_INFO()  (::basicstation_analytics::LogMessage(::basicstation_analytics::LogLevel::kInfo).stream())
#define LOG_WARN()  (::basicstation_analytics::LogMessage(::basicstation_analytics::LogLevel::kWarn).stream())
#define LOG_ERROR() (::basicstation_analytics::LogMessage(::basicstation_analytics::LogLevel::kError).stream())

}  // namespace basicstation_analytics
