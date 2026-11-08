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
  receiver.Configure(cfg.socket_rcvbuf_bytes,cfg.poll_max_datagrams);
  std::string open_err;
  if (!receiver.Open(cfg.socket_path, cfg.socket_mode, &open_err)) {
    LOG_ERROR() << "Failed to open socket " << cfg.socket_path << ": " << open_err;
    return 1;
  }
  receiver.SetGatewayId(cfg.gateway_id);
  LOG_INFO() << "Listening on " << cfg.socket_path;

  client_cc::AnalyticsClientConfig client_cfg;
  client_cfg.client_id=cfg.client_id;client_cfg.client_key=cfg.client_key;
  client_cfg.endpoint=cfg.endpoint;client_cfg.ca_file=cfg.ca_file;
  client_cfg.ssl_target_name_override=cfg.ssl_target_name_override;
  client_cfg.connect_timeout=cfg.connect_timeout;client_cfg.request_timeout=cfg.request_timeout;
  client_cfg.max_reconnect_backoff=cfg.max_reconnect_backoff;client_cfg.auto_reconnect=false;
  client_cc::Client client(client_cfg);
  BatchQueue queue(cfg.max_batches,cfg.max_messages_per_batch,cfg.max_buffer_bytes,true,cfg.pre_identity_max_messages);
  ParsedGatewaySync parsed;std::string sync_error;
  const bool do_sync=ParseGatewaySyncConfig(cfg,parsed,sync_error)&&parsed.has_any;
  std::unique_ptr<client_cc::Client> sync_client;
  if(do_sync)sync_client=std::make_unique<client_cc::Client>(client_cfg);
  std::thread sync_thread;std::atomic<bool> sync_started{false};
  Sender sender(&client,&queue,cfg.connect_timeout,cfg.request_timeout,cfg.max_reconnect_backoff,[&]{
    if(!do_sync||sync_started.exchange(true))return;
    sync_thread=std::thread([&]{
      auto deadline=std::chrono::system_clock::now()+std::chrono::seconds(cfg.connect_timeout+cfg.request_timeout);
      auto status=sync_client->ConnectOnce(deadline);
      if(status.ok())status=sync_client->GatewayUpsertOnce(ToUpsertRequest(parsed),deadline);
      if(!status.ok())LOG_WARN()<<"GatewayUpsert failed: "<<status.error_message();
      sync_client->Disconnect();
    });
  });
  sender.Start();
  auto last_flush=std::chrono::steady_clock::now();
  while(g_running.load()){
    struct pollfd pfd={};pfd.fd=receiver.Fd();pfd.events=POLLIN;
    int result=poll(&pfd,1,100);
    if(result>0&&(pfd.revents&POLLIN)){
      receiver.PollEach([&](api::AnalyticsMetrics&& record){
        if(receiver.HasIdentity())queue.SetIdentity(cfg.gateway_id,receiver.GatewayEuiBytes());
        queue.Add(std::move(record));
      });
      if(receiver.HasIdentity())queue.SetIdentity(cfg.gateway_id,receiver.GatewayEuiBytes());
    }
    auto now=std::chrono::steady_clock::now();
    if(now-last_flush>=std::chrono::seconds(cfg.batch_sec)){queue.Seal();last_flush=now;}
  }
  const auto deadline=std::chrono::steady_clock::now()+std::chrono::milliseconds(cfg.shutdown_deadline_ms);
  if(sync_client)sync_client->CancelPending();
  bool drained=sender.Shutdown(std::max(0,static_cast<int>(std::chrono::duration_cast<std::chrono::milliseconds>(deadline-std::chrono::steady_clock::now()).count())));
  if(sync_thread.joinable())sync_thread.join();
  receiver.Close();
  LOG_INFO()<<"stopped rejected="<<receiver.Rejected()<<" dropped_messages="<<queue.DroppedMessages()<<" retained_bytes="<<queue.Bytes()<<" push_failures="<<sender.PushFailures();
  return drained?0:2;
}
