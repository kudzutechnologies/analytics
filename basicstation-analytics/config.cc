/* SPDX-License-Identifier: MIT */
#include "config.h"
#include "gateway_sync.h"
#include <cerrno>
#include <climits>
#include <cstdlib>
#include <fstream>
#include <functional>
#include <map>
#include <sstream>
#include <stdexcept>

namespace basicstation_analytics {
namespace {
std::string Trim(std::string s) {
  auto a = s.find_first_not_of(" \t\r\n");
  if (a == std::string::npos)
    return "";
  s = s.substr(a, s.find_last_not_of(" \t\r\n") - a + 1);
  if (s.size() >= 2 && ((s.front() == '"' && s.back() == '"') ||
                        (s.front() == '\'' && s.back() == '\'')))
    s = s.substr(1, s.size() - 2);
  return s;
}
std::string Canonical(const std::string &k) {
  static const std::map<std::string, std::string> a = {
      {"s", "socket"},
      {"analytics-sock", "socket"},
      {"i", "client-id"},
      {"k", "client-key"},
      {"c", "config"},
      {"b", "batch-sec"},
      {"e", "analytics-endpoint"},
      {"endpoint", "analytics-endpoint"},
      {"flush-interval", "batch-sec"},
      {"ca-file", "analytics-ca-file"},
      {"ssl-target-name", "analytics-ssl-target-name"},
      {"connect-timeout", "analytics-connect-timeout"},
      {"request-timeout", "analytics-request-timeout"},
      {"max-backoff", "analytics-max-backoff"}};
  auto it = a.find(k);
  return it == a.end() ? k : it->second;
}
int Number(const std::string &v, int base = 10) {
  if (v.empty())
    throw std::runtime_error("empty integer");
  errno = 0;
  char *end = nullptr;
  long long n = std::strtoll(v.c_str(), &end, base);
  if (errno || !end || *end || n < INT_MIN || n > INT_MAX)
    throw std::runtime_error("invalid integer: " + v);
  return static_cast<int>(n);
}
bool Boolean(const std::string &v) {
  if (v == "true" || v == "1" || v == "yes")
    return true;
  if (v == "false" || v == "0" || v == "no")
    return false;
  throw std::runtime_error("invalid boolean: " + v);
}
using Values = std::map<std::string, std::string>;
void Put(Values &values, const std::string &key, const std::string &value) {
  auto k = Canonical(key);
  auto it = values.find(k);
  if (it != values.end() && it->second != value)
    throw std::runtime_error("conflicting values for " + k);
  values[k] = value;
}
} // namespace

bool ParseConfig(int argc, char *argv[], Config &out, std::string *error) {
  out = Config{};
  if (error)
    error->clear();
  try {
    Values cli;
    for (int i = 1; i < argc; ++i) {
      std::string a = argv[i];
      if (a == "--help" || a == "-h") {
        out.show_help = true;
        return false;
      }
      if (a == "--version") {
        out.show_version = true;
        return false;
      }
      if (a.empty() || a[0] != '-')
        throw std::runtime_error("unexpected argument: " + a);
      a.erase(0, a.find_first_not_of('-'));
      auto eq = a.find('=');
      std::string k = a.substr(0, eq), v;
      if (eq != std::string::npos)
        v = a.substr(eq + 1);
      else if (k == "gauge-stat" || k == "write")
        v = "true";
      else {
        if (i + 1 == argc)
          throw std::runtime_error("missing value for " + k);
        v = argv[++i];
      }
      Put(cli, k, v);
    }
    auto config = cli.find("config");
    if (config != cli.end())
      out.config_file_path = config->second;
    else if (const char *v = std::getenv("CONFIG"))
      out.config_file_path = v;
    Values file;
    if (!out.config_file_path.empty()) {
      std::ifstream in(out.config_file_path);
      const bool new_pairing_file =
          !in && errno == ENOENT && cli.count("pair-pin") && cli.count("write");
      if (!in && !new_pairing_file)
        throw std::runtime_error("cannot read config: " + out.config_file_path);
      std::string line;
      while (std::getline(in, line)) {
        line = Trim(line);
        if (line.empty() || line[0] == '#' || line[0] == ';')
          continue;
        auto eq = line.find('=');
        if (eq == std::string::npos)
          throw std::runtime_error("expected key=value");
        Put(file, Trim(line.substr(0, eq)), Trim(line.substr(eq + 1)));
      }
      if (in.bad())
        throw std::runtime_error("config read failed");
    }
    std::map<std::string, std::function<void(const std::string &)>> setters;
    auto str = [&](const char *k, std::string &dst) {
      setters[k] = [&dst](const std::string &v) { dst = v; };
    };
    auto num = [&](const char *k, int &dst) {
      setters[k] = [&dst](const std::string &v) { dst = Number(v); };
    };
    str("socket", out.socket_path);
    str("client-id", out.client_id);
    str("client-key", out.client_key);
    str("analytics-endpoint", out.endpoint);
    str("pairing-endpoint", out.pairing_endpoint);
    str("analytics-ca-file", out.ca_file);
    str("analytics-ssl-target-name", out.ssl_target_name_override);
    str("gateway", out.gateway_id);
    str("gateway-eid", out.gateway_eid);
    str("gateway-eui", out.gateway_eui);
    str("gateway-name", out.gateway_name);
    str("gateway-location", out.gateway_location);
    str("gateway-radios", out.gateway_radios);
    str("log-level", out.log_level);
    str("log-file", out.log_file);
    str("pair-pin", out.pair_pin);
    str("config", out.config_file_path);
    num("analytics-connect-timeout", out.connect_timeout);
    num("analytics-request-timeout", out.request_timeout);
    num("analytics-max-backoff", out.max_reconnect_backoff);
    num("batch-sec", out.batch_sec);
    num("max-batches", out.max_batches);
    num("max-messages", out.max_messages_per_batch);
    num("max-buffer-bytes", out.max_buffer_bytes);
    num("pre-identity-max-messages", out.pre_identity_max_messages);
    num("socket-rcvbuf-bytes", out.socket_rcvbuf_bytes);
    num("poll-max-datagrams", out.poll_max_datagrams);
    num("shutdown-deadline-ms", out.shutdown_deadline_ms);
    setters["socket-mode"] = [&](const std::string &v) {
      int n = Number(v, 8);
      if (n < 0 || n > 0777)
        throw std::runtime_error("invalid socket-mode");
      out.socket_mode = n;
    };
    setters["gauge-stat"] = [&](const std::string &v) {
      out.gauge_stat = Boolean(v);
    };
    setters["write"] = [&](const std::string &v) {
      out.write_config = Boolean(v);
    };
    auto apply = [&](const Values &values) {
      for (const auto &p : values) {
        auto it = setters.find(p.first);
        if (it == setters.end())
          throw std::runtime_error("unknown option: " + p.first);
        it->second(p.second);
      }
    };
    apply(file);
    Values env;
    for (const auto &p : setters) {
      std::string key = p.first;
      for (auto &c : key)
        c = c == '-' ? '_'
                     : static_cast<char>(
                           std::toupper(static_cast<unsigned char>(c)));
      if (p.first == "socket")
        key = "ANALYTICS_SOCK";
      if (const char *v = std::getenv(key.c_str()))
        Put(env, p.first, v);
    }
    if (const char *v = std::getenv("FLUSH_INTERVAL"))
      Put(env, "batch-sec", v);
    apply(env);
    apply(cli);
    if (out.batch_sec < 1 || out.max_batches < 1 ||
        out.max_messages_per_batch < 1 || out.max_buffer_bytes < 1024 ||
        out.pre_identity_max_messages < 0 || out.socket_rcvbuf_bytes < 1024 ||
        out.poll_max_datagrams < 1 || out.shutdown_deadline_ms < 0 ||
        out.connect_timeout < 1 || out.request_timeout < 1 ||
        out.max_reconnect_backoff < 1)
      throw std::runtime_error("invalid resource limits");
    if (out.log_level != "debug" && out.log_level != "info" &&
        out.log_level != "warn" && out.log_level != "error")
      throw std::runtime_error("invalid log-level");
    if (!out.pair_pin.empty()) {
      if (out.write_config && out.config_file_path.empty())
        throw std::runtime_error("--write requires --config");
      return true;
    }
    if (out.socket_path.empty() || out.client_id.empty() ||
        out.client_key.empty())
      throw std::runtime_error(
          "Missing required --socket/-s, --client-id/-i, or --client-key/-k");
    if (out.gateway_id.empty())
      throw std::runtime_error("--gateway is required (Kudzu ObjectID)");
    ParsedGatewaySync parsed;
    std::string sync_error;
    if (!ParseGatewaySyncConfig(out, parsed, sync_error) && !sync_error.empty())
      throw std::runtime_error(sync_error);
    return true;
  } catch (const std::exception &e) {
    if (error)
      *error = e.what();
    return false;
  }
}
} // namespace basicstation_analytics
