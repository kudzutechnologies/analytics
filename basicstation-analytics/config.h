/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#pragma once

#include "AnalyticsPairing.h"
#include "Client.h"

#include <cstdint>
#include <string>
#include <sys/types.h>

namespace basicstation_analytics {

#ifndef BS_ANALYTICS_VERSION
#define BS_ANALYTICS_VERSION "0.1.0"
#endif
#ifndef BS_ANALYTICS_GIT_HASH
#define BS_ANALYTICS_GIT_HASH "unknown"
#endif

struct Config {
  std::string socket_path;
  mode_t socket_mode = 0660;
  std::string client_id;
  std::string client_key;
  std::string endpoint = client_cc::kDefaultAnalyticsEndpoint;
  std::string pairing_endpoint = client_cc::DefaultPairingEndpoint();
  std::string ca_file;
  std::string ssl_target_name_override;
  int connect_timeout = 30;
  int request_timeout = 0;
  int max_reconnect_backoff = 60;
  int batch_sec = 10;
  int max_batches = 32;
  int max_messages_per_batch = 500;
  int shutdown_deadline_ms = 5000;
  std::string gateway_id;
  std::string gateway_eid;
  std::string gateway_eui;
  std::string gateway_name;
  std::string gateway_location;
  std::string gateway_radios;
  bool gauge_stat = false;
  std::string log_level = "info";
  std::string log_file;
  std::string pair_pin;
  std::string config_file_path;
  bool write_config = false;
  bool show_version = false;
  bool show_help = false;
};

// Returns false on help/version (caller exits 0) or parse abort.
// validation_error non-empty => exit 1.
bool ParseConfig(int argc, char* argv[], Config& out, std::string* validation_error = nullptr);

}  // namespace basicstation_analytics
