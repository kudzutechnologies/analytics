/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "batch_queue.h"
#include "config.h"
#include "gateway_sync.h"
#include "logging.h"
#include "pairing.h"
#include "receiver.h"
#include "sender.h"
#include "Client.h"
#include "protobuf/analytics.pb.h"

#include <atomic>
#include <chrono>
#include <csignal>
#include <fstream>
#include <iostream>
#include <poll.h>
#include <string>
#include <thread>

namespace {

std::atomic<int> g_running{1};

void SignalHandler(int) { g_running = 0; }

void Usage(const char* prog) {
  std::cerr
      << "Usage: " << prog << " [options]\n"
      << "  -s, --socket PATH           Unix socket path (required)\n"
      << "  -i, --client-id ID          Analytics client ID (hex, required)\n"
      << "  -k, --client-key KEY        Analytics client key (hex, required)\n"
      << "  -e, --endpoint HOST:PORT    Analytics gRPC endpoint "
         "(alias: --analytics-endpoint)\n"
      << "  -b, --batch-sec N           Flush batch every N seconds (default: 10)\n"
      << "  -c, --config FILE           INI config file\n"
      << "      --gateway ID            Kudzu gateway ObjectID (required)\n"
      << "      --gateway-eid/--gateway-eui/--gateway-name/--gateway-location/--gateway-radios\n"
      << "      --ca-file PATH          Extra CA PEM\n"
      << "      --ssl-target-name NAME  TLS SNI override\n"
      << "      --pair-pin PIN          Pair and print/write config\n"
      << "      --pairing-endpoint URL  Pairing HTTPS base URL\n"
      << "      --write                Write paired config to --config path\n"
      << "      --log-level/--log-file\n"
      << "      --version\n"
      << "  -h, --help\n";
}

void ScheduleGatewayUpsert(client_cc::Client* client, const basicstation_analytics::Config& cfg) {
  basicstation_analytics::ParsedGatewaySync parsed;
  std::string err;
  if (!basicstation_analytics::ParseGatewaySyncConfig(cfg, parsed, err)) {
    if (!err.empty()) LOG_WARN() << "Skipping gateway upsert: " << err;
    return;
  }
  if (!parsed.has_any) return;
  std::thread([client, parsed] {
    auto req = basicstation_analytics::ToUpsertRequest(parsed);
    if (!client->GatewayUpsert(req)) {
      LOG_WARN() << "GatewayUpsert failed: " << client->LastError();
    } else {
      LOG_INFO() << "GatewayUpsert completed";
    }
  }).detach();
}

}  // namespace

int main(int argc, char* argv[]) {
  using namespace basicstation_analytics;

  Config cfg;
  std::string validation_error;
  if (!ParseConfig(argc, argv, cfg, &validation_error)) {
    if (cfg.show_help) {
      Usage(argv[0]);
      return 0;
    }
    if (cfg.show_version) {
      std::cout << "basicstation-analytics " << BS_ANALYTICS_VERSION << " (" << BS_ANALYTICS_GIT_HASH
                << ")\n";
      return 0;
    }
    if (!validation_error.empty()) {
      std::cerr << "basicstation-analytics: " << validation_error << "\n";
      return 1;
    }
    Usage(argv[0]);
    return 1;
  }

  InitLogging(cfg.log_level, cfg.log_file);

  if (!cfg.pair_pin.empty()) {
    std::string error_msg;
    std::string ini = GetRenderedPairConfig(cfg.pair_pin, cfg, error_msg);
    if (ini.empty()) {
      std::cerr << "basicstation-analytics: Could not pair: " << error_msg << "\n";
      return 1;
    }
    if (cfg.write_config && !cfg.config_file_path.empty()) {
      std::ofstream f(cfg.config_file_path);
      if (!f) {
        std::cerr << "basicstation-analytics: Could not write " << cfg.config_file_path << "\n";
        return 1;
      }
      f << ini;
    } else {
      std::cout << ini;
    }
    return 0;
  }

  signal(SIGINT, SignalHandler);
  signal(SIGTERM, SignalHandler);

  Receiver receiver;
  std::string open_err;
  if (!receiver.Open(cfg.socket_path, cfg.socket_mode, &open_err)) {
    LOG_ERROR() << "Failed to open socket " << cfg.socket_path << ": " << open_err;
    return 1;
  }
  receiver.SetGatewayId(cfg.gateway_id);
  LOG_INFO() << "Listening on " << cfg.socket_path;

  client_cc::AnalyticsClientConfig client_cfg;
  client_cfg.client_id = cfg.client_id;
  client_cfg.client_key = cfg.client_key;
  client_cfg.endpoint = cfg.endpoint;
  client_cfg.ca_file = cfg.ca_file;
  client_cfg.ssl_target_name_override = cfg.ssl_target_name_override;
  client_cfg.connect_timeout = cfg.connect_timeout > 0 ? cfg.connect_timeout : 30;
  client_cfg.request_timeout = cfg.request_timeout;
  client_cfg.max_reconnect_backoff =
      cfg.max_reconnect_backoff > 0 ? cfg.max_reconnect_backoff : 60;
  client_cc::Client client(client_cfg);
  BatchQueue queue(static_cast<size_t>(cfg.max_batches));
  Sender sender(&client, &queue);
  sender.Start();

  // Background connect + gateway sync once connected.
  std::thread([&] {
    while (g_running.load()) {
      if (client.Connect()) {
        LOG_INFO() << "Connected to analytics";
        ScheduleGatewayUpsert(&client, cfg);
        return;
      }
      LOG_WARN() << "Connect failed: " << client.LastError();
      std::this_thread::sleep_for(std::chrono::seconds(5));
    }
  }).detach();

  api::AnalyticsMetrics batch;
  auto last_flush = std::chrono::steady_clock::now();
  const auto batch_interval = std::chrono::seconds(cfg.batch_sec);
  // Hold telemetry until EUI HELLO arrives.
  api::AnalyticsMetrics pending;

  auto flush_if_ready = [&](bool force) {
    if (!receiver.HasIdentity()) return;
    if (pending.uplinks_size() || pending.downlinks_size() || pending.stats_size()) {
      batch.mutable_uplinks()->MergeFrom(pending.uplinks());
      batch.mutable_downlinks()->MergeFrom(pending.downlinks());
      batch.mutable_stats()->MergeFrom(pending.stats());
      pending.Clear();
    }
    const int n = batch.uplinks_size() + batch.downlinks_size() + batch.stats_size();
    if (n == 0) return;
    auto now = std::chrono::steady_clock::now();
    if (!force && n < cfg.max_messages_per_batch && now - last_flush < batch_interval) return;

    if (!cfg.gateway_id.empty()) batch.set_gatewayid(cfg.gateway_id);
    batch.set_gatewayeui(receiver.GatewayEuiBytes());
    if (cfg.gauge_stat) {
      for (int i = 0; i < batch.stats_size(); ++i) batch.mutable_stats(i)->set_isgauge(true);
    }
    if (queue.Push(batch)) {
      LOG_WARN() << "queue full; dropped oldest batch (total drops=" << queue.Drops() << ")";
    }
    batch.Clear();
    last_flush = now;
  };

  while (g_running.load()) {
    struct pollfd pfd = {};
    pfd.fd = receiver.Fd();
    pfd.events = POLLIN;
    int pr = poll(&pfd, 1, 100);
    if (pr > 0 && (pfd.revents & POLLIN)) {
      api::AnalyticsMetrics scratch;
      auto st = receiver.Poll(&scratch);
      if (st.rejected) {
        LOG_DEBUG() << "rejected datagrams=" << st.rejected;
      }
      if (scratch.uplinks_size() || scratch.downlinks_size() || scratch.stats_size()) {
        if (receiver.HasGatewayEui()) {
          batch.mutable_uplinks()->MergeFrom(scratch.uplinks());
          batch.mutable_downlinks()->MergeFrom(scratch.downlinks());
          batch.mutable_stats()->MergeFrom(scratch.stats());
        } else {
          pending.mutable_uplinks()->MergeFrom(scratch.uplinks());
          pending.mutable_downlinks()->MergeFrom(scratch.downlinks());
          pending.mutable_stats()->MergeFrom(scratch.stats());
        }
      }
    }
    flush_if_ready(false);
  }

  flush_if_ready(true);
  sender.Stop();
  bool drained = sender.Drain(cfg.shutdown_deadline_ms);
  if (!drained) {
    LOG_WARN() << "shutdown drain incomplete; queue size=" << queue.Size();
  }
  client.Disconnect();
  receiver.Close();
  LOG_INFO() << "stopped rejected=" << receiver.Rejected() << " queue_drops=" << queue.Drops()
             << " push_failures=" << sender.PushFailures();
  return drained ? 0 : 2;
}
