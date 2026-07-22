#pragma once

#include "AnalyticsPairing.h"
#include "Client.h"

#include <cstdint>
#include <string>

namespace forwarder_cc {

inline constexpr const char* kForwarderVersion = "0.1.12";
#ifndef FORWARDER_CC_GIT_HASH
#define FORWARDER_CC_GIT_HASH "unknown"
#endif

struct Config {
  // UDP proxy
  int buffer_size = 1500;
  std::string listen_host = "127.0.0.1";
  int listen_port_up = 1800;
  int listen_port_down = 1801;
  std::string connect_host;
  int connect_port_up = 1700;
  int connect_port_down = 1700;
  std::string connect_interface = "0.0.0.0";
  int max_udp_streams = 0;
  int connect_retry_interval = 1;

  // Analytics client
  std::string client_id;
  std::string client_key;
  std::string endpoint = client_cc::kDefaultAnalyticsEndpoint;
  std::string pairing_endpoint = client_cc::DefaultPairingEndpoint();
  std::string ca_file;  // path to CA cert for TLS (analytics gRPC)
  std::string ssl_target_name_override;  // TLS server name for verification
  int connect_timeout = 0;
  int request_timeout = 0;
  int max_reconnect_backoff = 0;

  // Forwarder
  int flush_interval = 0;
  std::string gateway_id;
  std::string gateway_eid;
  std::string gateway_eui;
  std::string gateway_name;
  std::string gateway_location;
  std::string gateway_radios;
  bool gauge_stat = false;
  bool server_side = false;

  // Logging / debug
  std::string log_level = "info";
  std::string log_file;
  std::string debug_dump;

  // Pairing (if pair_pin set, fetch config and exit)
  std::string pair_pin;
  std::string config_file_path;  // for --write when pairing
  bool write_config = false;
};

// Parse from argc/argv and environment. Returns false on --version/--help (or parse abort).
// Sets validation_error when required runtime validation fails (caller should exit 1).
bool ParseConfig(int argc, char* argv[], Config& out, std::string* validation_error = nullptr);

}  // namespace forwarder_cc
