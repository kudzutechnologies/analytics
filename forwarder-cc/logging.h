#pragma once

#include <ostream>
#include <sstream>
#include <string>

namespace forwarder_cc {

enum class LogLevel { kDebug = 0, kInfo = 1, kWarn = 2, kError = 3 };

void InitLogging(const std::string& level_name, const std::string& log_file = "");
void SetLogLevel(LogLevel level);
LogLevel ParseLogLevel(const std::string& name);
LogLevel CurrentLogLevel();

// Temporary logger; lives until end of the full expression using stream().
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

#define LOG_DEBUG() (::forwarder_cc::LogMessage(::forwarder_cc::LogLevel::kDebug).stream())
#define LOG_INFO()  (::forwarder_cc::LogMessage(::forwarder_cc::LogLevel::kInfo).stream())
#define LOG_WARN()  (::forwarder_cc::LogMessage(::forwarder_cc::LogLevel::kWarn).stream())
#define LOG_ERROR() (::forwarder_cc::LogMessage(::forwarder_cc::LogLevel::kError).stream())

}  // namespace forwarder_cc
