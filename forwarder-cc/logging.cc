#include "logging.h"

#include <fstream>
#include <iostream>
#include <mutex>
#include <sstream>

namespace forwarder_cc {
namespace {

LogLevel g_level = LogLevel::kInfo;
std::ofstream g_file;
std::ostream* g_out = &std::cerr;
std::mutex g_mu;
std::ostringstream g_null;

}  // namespace

LogLevel ParseLogLevel(const std::string& name) {
  if (name == "debug" || name == "DEBUG") return LogLevel::kDebug;
  if (name == "info" || name == "INFO") return LogLevel::kInfo;
  if (name == "warn" || name == "warning" || name == "WARN" || name == "WARNING")
    return LogLevel::kWarn;
  if (name == "error" || name == "ERROR") return LogLevel::kError;
  return LogLevel::kInfo;
}

void SetLogLevel(LogLevel level) { g_level = level; }

LogLevel CurrentLogLevel() { return g_level; }

void InitLogging(const std::string& level_name, const std::string& log_file) {
  g_level = ParseLogLevel(level_name);
  if (!log_file.empty()) {
    g_file.open(log_file, std::ios::out | std::ios::app);
    if (g_file) {
      g_out = &g_file;
      return;
    }
  }
  g_out = &std::cerr;
}

LogMessage::LogMessage(LogLevel level)
    : level_(level),
      enabled_(static_cast<int>(level) >= static_cast<int>(g_level)) {}

LogMessage::~LogMessage() {
  if (!enabled_) return;
  const char* tag = "INFO";
  switch (level_) {
    case LogLevel::kDebug: tag = "DEBUG"; break;
    case LogLevel::kInfo:  tag = "INFO"; break;
    case LogLevel::kWarn:  tag = "WARN"; break;
    case LogLevel::kError: tag = "ERROR"; break;
  }
  std::lock_guard<std::mutex> lock(g_mu);
  (*g_out) << tag << ": " << buf_.str() << std::endl;
}

std::ostream& LogMessage::stream() {
  if (enabled_) return buf_;
  g_null.str("");
  g_null.clear();
  return g_null;
}

}  // namespace forwarder_cc
