#include "config.h"
#include "gateway_sync.h"

#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iostream>
#include <sstream>

namespace forwarder_cc {

namespace {

std::string GetEnv(const char* name, const std::string& fallback) {
  const char* v = std::getenv(name);
  if (v && v[0]) return v;
  return fallback;
}

int GetEnvInt(const char* name, int fallback) {
  const char* v = std::getenv(name);
  if (!v || !v[0]) return fallback;
  return std::atoi(v);
}

bool GetEnvBool(const char* name, bool fallback) {
  const char* v = std::getenv(name);
  if (!v || !v[0]) return fallback;
  if (v[0] == '1' || v[0] == 't' || v[0] == 'T' || v[0] == 'y' || v[0] == 'Y') return true;
  if (std::strcmp(v, "true") == 0 || std::strcmp(v, "TRUE") == 0) return true;
  return false;
}

static std::string Trim(const std::string& s) {
  size_t start = s.find_first_not_of(" \t\r\n");
  if (start == std::string::npos) return "";
  size_t end = s.find_last_not_of(" \t\r\n");
  return s.substr(start, end == std::string::npos ? std::string::npos : end - start + 1);
}

static void ApplyConfigKey(Config& c, const std::string& key, const std::string& value) {
  if (key == "buffer-size") { c.buffer_size = std::atoi(value.c_str()); return; }
  if (key == "listen-host") { c.listen_host = value; return; }
  if (key == "listen-port-up") { c.listen_port_up = std::atoi(value.c_str()); return; }
  if (key == "listen-port-down") { c.listen_port_down = std::atoi(value.c_str()); return; }
  if (key == "connect-host") { c.connect_host = value; return; }
  if (key == "connect-port-up") { c.connect_port_up = std::atoi(value.c_str()); return; }
  if (key == "connect-port-down") { c.connect_port_down = std::atoi(value.c_str()); return; }
  if (key == "connect-interface") { c.connect_interface = value; return; }
  if (key == "max-udp-streams") { c.max_udp_streams = std::atoi(value.c_str()); return; }
  if (key == "connect-retry-interval") { c.connect_retry_interval = std::atoi(value.c_str()); return; }
  if (key == "client-id") { c.client_id = value; return; }
  if (key == "client-key") { c.client_key = value; return; }
  if (key == "analytics-endpoint") { c.endpoint = value; return; }
  if (key == "analytics-ca-file") { c.ca_file = value; return; }
  if (key == "analytics-ssl-target-name") { c.ssl_target_name_override = value; return; }
  if (key == "analytics-connect-timeout") { c.connect_timeout = std::atoi(value.c_str()); return; }
  if (key == "analytics-request-timeout") { c.request_timeout = std::atoi(value.c_str()); return; }
  if (key == "analytics-max-backoff") { c.max_reconnect_backoff = std::atoi(value.c_str()); return; }
  if (key == "flush-interval") { c.flush_interval = std::atoi(value.c_str()); return; }
  if (key == "gateway") { c.gateway_id = value; return; }
  if (key == "gateway-eid") { c.gateway_eid = value; return; }
  if (key == "gateway-eui") { c.gateway_eui = value; return; }
  if (key == "gateway-name") { c.gateway_name = value; return; }
  if (key == "gateway-location") { c.gateway_location = value; return; }
  if (key == "gateway-radios") { c.gateway_radios = value; return; }
  if (key == "gauge-stat") { c.gauge_stat = (value == "true" || value == "1"); return; }
  if (key == "server-side") { c.server_side = (value == "true" || value == "1"); return; }
  if (key == "log-level") { c.log_level = value; return; }
  if (key == "log-file") { c.log_file = value; return; }
  if (key == "debug-dump") { c.debug_dump = value; return; }
  if (key == "config") { c.config_file_path = value; return; }
}

bool LoadConfigFile(const std::string& path, Config& c) {
  std::ifstream f(path);
  if (!f) return false;
  std::string line;
  while (std::getline(f, line)) {
    line = Trim(line);
    if (line.empty() || line[0] == '#') continue;
    size_t eq = line.find('=');
    if (eq == std::string::npos) continue;
    std::string key = Trim(line.substr(0, eq));
    std::string value = Trim(line.substr(eq + 1));
    if (!key.empty()) ApplyConfigKey(c, key, value);
  }
  return true;
}

bool GetConfigFilePath(int argc, char* argv[], std::string& out_path) {
  for (int i = 1; i < argc; ++i) {
    const char* a = argv[i];
    if (a[0] != '-' || a[1] != '-') continue;
    a += 2;
    size_t key_len = std::strlen("config");
    if (std::strncmp(a, "config", key_len) != 0) continue;
    if (a[key_len] == '=') {
      out_path = a + key_len + 1;
      return true;
    }
    if (a[key_len] == '\0' && i + 1 < argc) {
      out_path = argv[i + 1];
      return true;
    }
  }
  return false;
}

void ApplyEnv(Config& c) {
  c.buffer_size = GetEnvInt("BUFFER_SIZE", c.buffer_size);
  c.listen_host = GetEnv("LISTEN_HOST", c.listen_host);
  c.listen_port_up = GetEnvInt("LISTEN_PORT_UP", c.listen_port_up);
  c.listen_port_down = GetEnvInt("LISTEN_PORT_DOWN", c.listen_port_down);
  c.connect_host = GetEnv("CONNECT_HOST", c.connect_host);
  c.connect_port_up = GetEnvInt("CONNECT_PORT_UP", c.connect_port_up);
  c.connect_port_down = GetEnvInt("CONNECT_PORT_DOWN", c.connect_port_down);
  c.connect_interface = GetEnv("CONNECT_INTERFACE", c.connect_interface);
  c.max_udp_streams = GetEnvInt("MAX_UDP_STREAMS", c.max_udp_streams);
  c.connect_retry_interval = GetEnvInt("CONNECT_RETRY_INTERVAL", c.connect_retry_interval);

  c.client_id = GetEnv("CLIENT_ID", c.client_id);
  c.client_key = GetEnv("CLIENT_KEY", c.client_key);
  c.endpoint = GetEnv("ANALYTICS_ENDPOINT", c.endpoint);
  c.ca_file = GetEnv("ANALYTICS_CA_FILE", c.ca_file);
  c.ssl_target_name_override = GetEnv("ANALYTICS_SSL_TARGET_NAME", c.ssl_target_name_override);
  c.connect_timeout = GetEnvInt("ANALYTICS_CONNECT_TIMEOUT", c.connect_timeout);
  c.request_timeout = GetEnvInt("ANALYTICS_REQUEST_TIMEOUT", c.request_timeout);
  c.max_reconnect_backoff = GetEnvInt("ANALYTICS_MAX_BACKOFF", c.max_reconnect_backoff);

  c.flush_interval = GetEnvInt("FLUSH_INTERVAL", c.flush_interval);
  c.gateway_id = GetEnv("GATEWAY", c.gateway_id);
  c.gateway_eid = GetEnv("GATEWAY_EID", c.gateway_eid);
  c.gateway_eui = GetEnv("GATEWAY_EUI", c.gateway_eui);
  c.gateway_name = GetEnv("GATEWAY_NAME", c.gateway_name);
  c.gateway_location = GetEnv("GATEWAY_LOCATION", c.gateway_location);
  c.gateway_radios = GetEnv("GATEWAY_RADIOS", c.gateway_radios);
  c.gauge_stat = GetEnvBool("GAUGE_STAT", c.gauge_stat);
  c.server_side = GetEnvBool("SERVER_SIDE", c.server_side);

  c.log_level = GetEnv("LOG_LEVEL", c.log_level);
  c.log_file = GetEnv("LOG_FILE", c.log_file);
  c.debug_dump = GetEnv("DEBUG_DUMP", c.debug_dump);
}

bool ParseArg(const char* arg, const char* key, std::string& value, bool& needs_next) {
  needs_next = false;
  size_t key_len = std::strlen(key);
  if (std::strncmp(arg, key, key_len) != 0) return false;
  if (arg[key_len] == '=') {
    value = arg + key_len + 1;
    return true;
  }
  if (arg[key_len] == '\0') {
    needs_next = true;
    return true;
  }
  return false;
}

bool TakeArgValue(int argc, char* argv[], int& i, bool needs_next, std::string& val) {
  if (!needs_next) return true;
  if (i + 1 >= argc) return false;
  val = argv[++i];
  return true;
}

}  // namespace

bool ParseConfig(int argc, char* argv[], Config& out, std::string* validation_error) {
  // Order: defaults -> config file -> env -> command-line
  std::string config_path;
  GetConfigFilePath(argc, argv, config_path);
  if (!config_path.empty()) {
    LoadConfigFile(config_path, out);
    out.config_file_path = config_path;
  }
  ApplyEnv(out);

  for (int i = 1; i < argc; ++i) {
    const char* a = argv[i];
    if (a[0] != '-' || a[1] != '-') continue;
    a += 2;

    std::string val;
    bool needs_next = false;

    auto handle = [&](const char* key, auto setter) -> bool {
      if (!ParseArg(a, key, val, needs_next)) return false;
      if (!TakeArgValue(argc, argv, i, needs_next, val)) return true;
      setter(val);
      return true;
    };

    if (handle("buffer-size", [&](const std::string& v) { out.buffer_size = std::atoi(v.c_str()); })) continue;
    if (handle("listen-host", [&](const std::string& v) { out.listen_host = v; })) continue;
    if (handle("listen-port-up", [&](const std::string& v) { out.listen_port_up = std::atoi(v.c_str()); })) continue;
    if (handle("listen-port-down", [&](const std::string& v) { out.listen_port_down = std::atoi(v.c_str()); })) continue;
    if (handle("connect-host", [&](const std::string& v) { out.connect_host = v; })) continue;
    if (handle("connect-port-up", [&](const std::string& v) { out.connect_port_up = std::atoi(v.c_str()); })) continue;
    if (handle("connect-port-down", [&](const std::string& v) { out.connect_port_down = std::atoi(v.c_str()); })) continue;
    if (handle("connect-interface", [&](const std::string& v) { out.connect_interface = v; })) continue;
    if (handle("max-udp-streams", [&](const std::string& v) { out.max_udp_streams = std::atoi(v.c_str()); })) continue;
    if (handle("connect-retry-interval", [&](const std::string& v) { out.connect_retry_interval = std::atoi(v.c_str()); })) continue;

    if (handle("client-id", [&](const std::string& v) { out.client_id = v; })) continue;
    if (handle("client-key", [&](const std::string& v) { out.client_key = v; })) continue;
    if (handle("analytics-endpoint", [&](const std::string& v) { out.endpoint = v; })) continue;
    if (handle("analytics-ca-file", [&](const std::string& v) { out.ca_file = v; })) continue;
    if (handle("analytics-ssl-target-name", [&](const std::string& v) { out.ssl_target_name_override = v; })) continue;
    if (handle("analytics-connect-timeout", [&](const std::string& v) { out.connect_timeout = std::atoi(v.c_str()); })) continue;
    if (handle("analytics-request-timeout", [&](const std::string& v) { out.request_timeout = std::atoi(v.c_str()); })) continue;
    if (handle("analytics-max-backoff", [&](const std::string& v) { out.max_reconnect_backoff = std::atoi(v.c_str()); })) continue;

    if (handle("flush-interval", [&](const std::string& v) { out.flush_interval = std::atoi(v.c_str()); })) continue;
    if (handle("gateway", [&](const std::string& v) { out.gateway_id = v; })) continue;
    if (handle("gateway-eid", [&](const std::string& v) { out.gateway_eid = v; })) continue;
    if (handle("gateway-eui", [&](const std::string& v) { out.gateway_eui = v; })) continue;
    if (handle("gateway-name", [&](const std::string& v) { out.gateway_name = v; })) continue;
    if (handle("gateway-location", [&](const std::string& v) { out.gateway_location = v; })) continue;
    if (handle("gateway-radios", [&](const std::string& v) { out.gateway_radios = v; })) continue;
    if (handle("gauge-stat", [&](const std::string& v) { out.gauge_stat = (v == "true" || v == "1"); })) continue;
    if (handle("server-side", [&](const std::string& v) { out.server_side = (v == "true" || v == "1"); })) continue;

    if (handle("log-level", [&](const std::string& v) { out.log_level = v; })) continue;
    if (handle("log-file", [&](const std::string& v) { out.log_file = v; })) continue;
    if (handle("debug-dump", [&](const std::string& v) { out.debug_dump = v; })) continue;
    if (handle("pair-pin", [&](const std::string& v) { out.pair_pin = v; })) continue;
    if (std::strcmp(a, "write") == 0 || std::strncmp(a, "write=", 6) == 0) {
      out.write_config = true;
      continue;
    }
    if (handle("config", [&](const std::string& v) { out.config_file_path = v; })) continue;

    if (std::strcmp(a, "version") == 0) {
      std::cout << "Kudzu Analytics UDP Packet Forwarder (C++) v" << kForwarderVersion
                << " (Git " << FORWARDER_CC_GIT_HASH << ")\n";
      return false;
    }
    if (std::strcmp(a, "help") == 0) {
      std::cout << "Usage: forwarder-cc [--config=FILE] [--connect-host=HOST] [--client-id=ID] "
                   "[--client-key=KEY] [--gateway=ID] ...\n"
                << "       [--gateway-eid=] [--gateway-eui=] [--gateway-name=] "
                   "[--gateway-location=] [--gateway-radios=]\n"
                << "       [--pair-pin=PIN] [--write]  fetch config from server and exit\n"
                << "       [--log-level=] [--log-file=] [--debug-dump=] [--connect-retry-interval=]\n";
      return false;
    }
  }

  if (out.max_udp_streams == 0) {
    out.max_udp_streams = out.server_side ? 256 : 2;
  }
  if (out.flush_interval == 0) {
    out.flush_interval = out.server_side ? 5 : 10;
  }
  if (out.connect_retry_interval <= 0) {
    out.connect_retry_interval = 1;
  }

  // Early gateway-sync validation (fatal when options present but invalid).
  ParsedGatewaySync parsed;
  std::string sync_err;
  bool ok = ParseGatewaySyncConfig(out, parsed, sync_err);
  if (!ok && !sync_err.empty()) {
    if (validation_error) *validation_error = "Invalid gateway sync configuration: " + sync_err;
    else std::cerr << "forwarder-cc: Invalid gateway sync configuration: " << sync_err << "\n";
    return false;
  }

  return true;
}

}  // namespace forwarder_cc
