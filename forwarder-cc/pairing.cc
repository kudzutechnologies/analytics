#include "pairing.h"
#include "AnalyticsPairing.h"

#include <map>
#include <sstream>

namespace forwarder_cc {

namespace {

using IniMap = std::map<std::string, std::string>;

void ConfigToIniMap(const Config& c, IniMap& out) {
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
  out["connect-host"] = c.connect_host;
  out["connect-port-up"] = std::to_string(c.connect_port_up);
  out["connect-port-down"] = std::to_string(c.connect_port_down);
  out["listen-host"] = c.listen_host;
  out["listen-port-up"] = std::to_string(c.listen_port_up);
  out["listen-port-down"] = std::to_string(c.listen_port_down);
  out["connect-interface"] = c.connect_interface;
  out["connect-retry-interval"] = std::to_string(c.connect_retry_interval);
  out["buffer-size"] = std::to_string(c.buffer_size);
  out["max-udp-streams"] = std::to_string(c.max_udp_streams);
  out["flush-interval"] = std::to_string(c.flush_interval);
  out["log-level"] = c.log_level;
  out["log-file"] = c.log_file;
  out["debug-dump"] = c.debug_dump;
  out["server-side"] = c.server_side ? "true" : "false";
  out["gauge-stat"] = c.gauge_stat ? "true" : "false";
}

Config DefaultConfig() {
  Config c;
  c.buffer_size = 1500;
  c.listen_host = "127.0.0.1";
  c.listen_port_up = 1800;
  c.listen_port_down = 1801;
  c.connect_port_up = 1700;
  c.connect_port_down = 1700;
  c.connect_interface = "0.0.0.0";
  c.connect_retry_interval = 1;
  c.connect_timeout = 0;
  c.request_timeout = 0;
  c.max_reconnect_backoff = 0;
  c.flush_interval = 0;
  c.gauge_stat = false;
  c.server_side = false;
  c.log_level = "info";
  return c;
}

std::string RenderIni(const Config& config, const std::map<std::string, std::string>& extras) {
  IniMap config_map;
  ConfigToIniMap(config, config_map);
  for (const auto& p : extras) {
    if (client_cc::IsProtectedPairingConfigKey(p.first)) continue;
    config_map[p.first] = p.second;
  }
  Config default_c = DefaultConfig();
  IniMap default_map;
  ConfigToIniMap(default_c, default_map);

  std::ostringstream ini;
  for (const auto& p : config_map) {
    auto it = default_map.find(p.first);
    if (it != default_map.end() && it->second == p.second) continue;
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

}  // namespace forwarder_cc
