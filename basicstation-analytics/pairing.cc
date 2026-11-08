/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "pairing.h"
#include "AnalyticsPairing.h"

#include <map>
#include <sstream>

namespace basicstation_analytics {
namespace {

using IniMap = std::map<std::string, std::string>;

void ConfigToIniMap(const Config& c, IniMap& out) {
  out["socket"] = c.socket_path;
  out["client-id"] = c.client_id;
  out["client-key"] = c.client_key;
  out["gateway"] = c.gateway_id;
  out["gateway-eid"] = c.gateway_eid;
  out["gateway-eui"] = c.gateway_eui;
  out["gateway-name"] = c.gateway_name;
  out["gateway-location"] = c.gateway_location;
  out["gateway-radios"] = c.gateway_radios;
  out["analytics-endpoint"] = c.endpoint;
  out["pairing-endpoint"] = c.pairing_endpoint;
  out["analytics-ca-file"] = c.ca_file;
  out["analytics-ssl-target-name"] = c.ssl_target_name_override;
  out["batch-sec"] = std::to_string(c.batch_sec);
  out["max-batches"] = std::to_string(c.max_batches);
  out["log-level"] = c.log_level;
  out["log-file"] = c.log_file;
  out["gauge-stat"] = c.gauge_stat ? "true" : "false";
}

std::string RenderIni(const Config& config, const std::map<std::string, std::string>& extras) {
  IniMap config_map;
  ConfigToIniMap(config, config_map);
  for (const auto& p : extras) {
    if (client_cc::IsProtectedPairingConfigKey(p.first)) continue;
    config_map[p.first] = p.second;
  }
  std::ostringstream ini;
  for (const auto& p : config_map) {
    if (p.second.empty()) continue;
    if (p.first == "gauge-stat" && p.second == "false") continue;
    ini << p.first << "=" << p.second << "\n";
  }
  return ini.str();
}

}  // namespace

std::string RenderPairConfig(const Config& current_config,
                             const client_cc::PairingConfig& pair_config) {
  Config merged = current_config;
  merged.client_id = pair_config.client_id;
  merged.client_key = pair_config.client_key;
  merged.gateway_id = pair_config.gateway_id;
  return RenderIni(merged, pair_config.extras);
}

std::string GetRenderedPairConfig(const std::string& pin,
                                  const Config& current_config,
                                  std::string& error_msg) {
  client_cc::PairingOptions opts;
  opts.pin = pin;
  opts.endpoint = current_config.pairing_endpoint;
  opts.ca_file = current_config.ca_file;

  client_cc::PairingConfig pair_config;
  if (!client_cc::FetchPairingConfig(opts, pair_config, error_msg)) {
    return "";
  }

  return RenderPairConfig(current_config, pair_config);
}

}  // namespace basicstation_analytics
