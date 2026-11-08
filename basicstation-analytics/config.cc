/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "config.h"
#include "gateway_sync.h"

#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iostream>
#include <sstream>

namespace basicstation_analytics {
namespace {

std::string GetEnv(const char* key, const std::string& def = "") {
  const char* v = getenv(key);
  return v ? std::string(v) : def;
}

int GetEnvInt(const char* key, int def) {
  const char* v = getenv(key);
  return v ? std::atoi(v) : def;
}

void ApplyKey(Config& c, const std::string& key, const std::string& value) {
  if (key == "socket" || key == "analytics-sock") { c.socket_path = value; return; }
  if (key == "socket-mode") {
    c.socket_mode = static_cast<mode_t>(std::strtoul(value.c_str(), nullptr, 8));
    return;
  }
  if (key == "client-id") { c.client_id = value; return; }
  if (key == "client-key") { c.client_key = value; return; }
  if (key == "analytics-endpoint") { c.endpoint = value; return; }
  if (key == "pairing-endpoint") { c.pairing_endpoint = value; return; }
  if (key == "analytics-ca-file") { c.ca_file = value; return; }
  if (key == "analytics-ssl-target-name") { c.ssl_target_name_override = value; return; }
  if (key == "analytics-connect-timeout") { c.connect_timeout = std::atoi(value.c_str()); return; }
  if (key == "analytics-request-timeout") { c.request_timeout = std::atoi(value.c_str()); return; }
  if (key == "analytics-max-backoff") { c.max_reconnect_backoff = std::atoi(value.c_str()); return; }
  if (key == "batch-sec" || key == "flush-interval") { c.batch_sec = std::atoi(value.c_str()); return; }
  if (key == "max-batches") { c.max_batches = std::atoi(value.c_str()); return; }
  if (key == "max-messages") { c.max_messages_per_batch = std::atoi(value.c_str()); return; }
  if (key == "shutdown-deadline-ms") { c.shutdown_deadline_ms = std::atoi(value.c_str()); return; }
  if (key == "gateway") { c.gateway_id = value; return; }
  if (key == "gateway-eid") { c.gateway_eid = value; return; }
  if (key == "gateway-eui") { c.gateway_eui = value; return; }
  if (key == "gateway-name") { c.gateway_name = value; return; }
  if (key == "gateway-location") { c.gateway_location = value; return; }
  if (key == "gateway-radios") { c.gateway_radios = value; return; }
  if (key == "gauge-stat") { c.gauge_stat = (value == "1" || value == "true" || value == "yes"); return; }
  if (key == "log-level") { c.log_level = value; return; }
  if (key == "log-file") { c.log_file = value; return; }
}

bool LoadIni(const std::string& path, Config& c) {
  std::ifstream in(path);
  if (!in) return false;
  std::string line;
  while (std::getline(in, line)) {
    if (line.empty() || line[0] == '#' || line[0] == ';') continue;
    auto eq = line.find('=');
    if (eq == std::string::npos) continue;
    std::string key = line.substr(0, eq);
    std::string value = line.substr(eq + 1);
    while (!key.empty() && (key.back() == ' ' || key.back() == '\t')) key.pop_back();
    while (!value.empty() && (value.front() == ' ' || value.front() == '\t')) value.erase(value.begin());
    ApplyKey(c, key, value);
  }
  return true;
}

void ApplyEnv(Config& c) {
  c.socket_path = GetEnv("ANALYTICS_SOCK", c.socket_path);
  c.client_id = GetEnv("CLIENT_ID", c.client_id);
  c.client_key = GetEnv("CLIENT_KEY", c.client_key);
  c.endpoint = GetEnv("ANALYTICS_ENDPOINT", c.endpoint);
  c.pairing_endpoint = GetEnv("PAIRING_ENDPOINT", c.pairing_endpoint);
  c.ca_file = GetEnv("ANALYTICS_CA_FILE", c.ca_file);
  c.ssl_target_name_override = GetEnv("ANALYTICS_SSL_TARGET_NAME", c.ssl_target_name_override);
  c.connect_timeout = GetEnvInt("ANALYTICS_CONNECT_TIMEOUT", c.connect_timeout);
  c.request_timeout = GetEnvInt("ANALYTICS_REQUEST_TIMEOUT", c.request_timeout);
  c.max_reconnect_backoff = GetEnvInt("ANALYTICS_MAX_BACKOFF", c.max_reconnect_backoff);
  c.batch_sec = GetEnvInt("BATCH_SEC", c.batch_sec);
  c.max_batches = GetEnvInt("MAX_BATCHES", c.max_batches);
  c.gateway_id = GetEnv("GATEWAY", c.gateway_id);
  c.gateway_eid = GetEnv("GATEWAY_EID", c.gateway_eid);
  c.gateway_eui = GetEnv("GATEWAY_EUI", c.gateway_eui);
  c.gateway_name = GetEnv("GATEWAY_NAME", c.gateway_name);
  c.gateway_location = GetEnv("GATEWAY_LOCATION", c.gateway_location);
  c.gateway_radios = GetEnv("GATEWAY_RADIOS", c.gateway_radios);
  c.log_level = GetEnv("LOG_LEVEL", c.log_level);
  c.log_file = GetEnv("LOG_FILE", c.log_file);
}

}  // namespace

bool ParseConfig(int argc, char* argv[], Config& out, std::string* validation_error) {
  out = Config{};
  std::string ini_path;
  bool endpoint_set_by_cli = false;
  bool pairing_endpoint_set_by_cli = false;

  for (int i = 1; i < argc; ++i) {
    const char* a = argv[i];
    auto take = [&](const char* short_opt, const char* long_opt) -> const char* {
      if (short_opt && (std::strcmp(a, short_opt) == 0 || std::strcmp(a, long_opt) == 0)) {
        if (i + 1 < argc) return argv[++i];
      }
      if (long_opt && std::strcmp(a, long_opt) == 0) {
        if (i + 1 < argc) return argv[++i];
      }
      if (long_opt) {
        const size_t n = std::strlen(long_opt);
        if (std::strncmp(a, long_opt, n) == 0 && a[n] == '=') return a + n + 1;
      }
      return nullptr;
    };

    if (std::strcmp(a, "-h") == 0 || std::strcmp(a, "--help") == 0) {
      out.show_help = true;
      return false;
    }
    if (std::strcmp(a, "--version") == 0) {
      out.show_version = true;
      return false;
    }
    if (std::strcmp(a, "--write") == 0) {
      out.write_config = true;
      continue;
    }
    if (std::strcmp(a, "--gauge-stat") == 0) {
      out.gauge_stat = true;
      continue;
    }
    if (const char* v = take("-c", "--config")) {
      ini_path = v;
      out.config_file_path = v;
      continue;
    }
    if (const char* v = take("-s", "--socket")) { out.socket_path = v; continue; }
    if (const char* v = take("-i", "--client-id")) { out.client_id = v; continue; }
    if (const char* v = take("-k", "--client-key")) { out.client_key = v; continue; }
    if (const char* v = take("-e", "--endpoint")) {
      out.endpoint = v;
      endpoint_set_by_cli = true;
      continue;
    }
    if (const char* v = take(nullptr, "--analytics-endpoint")) {
      out.endpoint = v;
      endpoint_set_by_cli = true;
      continue;
    }
    if (const char* v = take(nullptr, "--pairing-endpoint")) {
      out.pairing_endpoint = v;
      pairing_endpoint_set_by_cli = true;
      continue;
    }
    if (const char* v = take("-b", "--batch-sec")) {
      out.batch_sec = std::atoi(v);
      continue;
    }
    if (const char* v = take(nullptr, "--ca-file")) { out.ca_file = v; continue; }
    if (const char* v = take(nullptr, "--ssl-target-name")) {
      out.ssl_target_name_override = v;
      continue;
    }
    if (const char* v = take(nullptr, "--gateway")) { out.gateway_id = v; continue; }
    if (const char* v = take(nullptr, "--gateway-eid")) { out.gateway_eid = v; continue; }
    if (const char* v = take(nullptr, "--gateway-eui")) { out.gateway_eui = v; continue; }
    if (const char* v = take(nullptr, "--gateway-name")) { out.gateway_name = v; continue; }
    if (const char* v = take(nullptr, "--gateway-location")) { out.gateway_location = v; continue; }
    if (const char* v = take(nullptr, "--gateway-radios")) { out.gateway_radios = v; continue; }
    if (const char* v = take(nullptr, "--pair-pin")) { out.pair_pin = v; continue; }
    if (const char* v = take(nullptr, "--log-level")) { out.log_level = v; continue; }
    if (const char* v = take(nullptr, "--log-file")) { out.log_file = v; continue; }
    if (const char* v = take(nullptr, "--max-batches")) {
      out.max_batches = std::atoi(v);
      continue;
    }
    if (const char* v = take(nullptr, "--connect-timeout")) {
      out.connect_timeout = std::atoi(v);
      continue;
    }
    if (const char* v = take(nullptr, "--request-timeout")) {
      out.request_timeout = std::atoi(v);
      continue;
    }
    if (const char* v = take(nullptr, "--max-backoff")) {
      out.max_reconnect_backoff = std::atoi(v);
      continue;
    }
    if (const char* v = take(nullptr, "--shutdown-deadline-ms")) {
      out.shutdown_deadline_ms = std::atoi(v);
      continue;
    }

    // Fallback key=value style
    const char* eq = std::strchr(a, '=');
    if (eq && a[0] == '-' && a[1] == '-') {
      ApplyKey(out, std::string(a + 2, eq), std::string(eq + 1));
      continue;
    }
  }

  // Precedence inside Apply*: CLI already applied; reload file+env underneath carefully.
  // Plan: defaults → INI → env → CLI. Rebuild from defaults then layer.
  Config layered;
  if (!ini_path.empty()) LoadIni(ini_path, layered);
  else if (!out.config_file_path.empty()) LoadIni(out.config_file_path, layered);
  ApplyEnv(layered);

  // Overlay CLI/non-default fields from first pass stored in `out`.
  auto overlay = [](std::string& dst, const std::string& src) {
    if (!src.empty()) dst = src;
  };
  overlay(layered.socket_path, out.socket_path);
  overlay(layered.client_id, out.client_id);
  overlay(layered.client_key, out.client_key);
  if (endpoint_set_by_cli) layered.endpoint = out.endpoint;
  if (pairing_endpoint_set_by_cli) layered.pairing_endpoint = out.pairing_endpoint;
  overlay(layered.ca_file, out.ca_file);
  overlay(layered.ssl_target_name_override, out.ssl_target_name_override);
  overlay(layered.gateway_id, out.gateway_id);
  overlay(layered.gateway_eid, out.gateway_eid);
  overlay(layered.gateway_eui, out.gateway_eui);
  overlay(layered.gateway_name, out.gateway_name);
  overlay(layered.gateway_location, out.gateway_location);
  overlay(layered.gateway_radios, out.gateway_radios);
  overlay(layered.log_level, out.log_level);
  overlay(layered.log_file, out.log_file);
  overlay(layered.pair_pin, out.pair_pin);
  overlay(layered.config_file_path, out.config_file_path);
  if (out.batch_sec != Config{}.batch_sec) layered.batch_sec = out.batch_sec;
  if (out.max_batches != Config{}.max_batches) layered.max_batches = out.max_batches;
  if (out.connect_timeout != Config{}.connect_timeout) layered.connect_timeout = out.connect_timeout;
  if (out.request_timeout != Config{}.request_timeout) layered.request_timeout = out.request_timeout;
  if (out.max_reconnect_backoff != Config{}.max_reconnect_backoff)
    layered.max_reconnect_backoff = out.max_reconnect_backoff;
  if (out.shutdown_deadline_ms != Config{}.shutdown_deadline_ms)
    layered.shutdown_deadline_ms = out.shutdown_deadline_ms;
  if (out.socket_mode != Config{}.socket_mode) layered.socket_mode = out.socket_mode;
  layered.gauge_stat = out.gauge_stat || layered.gauge_stat;
  layered.write_config = out.write_config;
  out = layered;

  if (out.batch_sec < 1) out.batch_sec = 1;
  if (out.max_batches < 1) out.max_batches = 1;

  if (!out.pair_pin.empty()) return true;

  if (out.socket_path.empty() || out.client_id.empty() || out.client_key.empty()) {
    if (validation_error) {
      *validation_error = "Missing required --socket/-s, --client-id/-i, or --client-key/-k";
    }
    return false;
  }
  if (out.gateway_id.empty()) {
    if (validation_error) {
      *validation_error = "--gateway is required (Kudzu ObjectID)";
    }
    return false;
  }

  ParsedGatewaySync parsed;
  std::string sync_err;
  if (!ParseGatewaySyncConfig(out, parsed, sync_err) && !sync_err.empty()) {
    if (validation_error) *validation_error = sync_err;
    return false;
  }
  return true;
}

}  // namespace basicstation_analytics
