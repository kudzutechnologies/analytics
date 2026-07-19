#include "config.h"
#include "forwarder.h"
#include "logging.h"
#include "pairing.h"
#include "udp_proxy.h"
#include "Client.h"

#include <cstdlib>
#include <fstream>
#include <iostream>

int main(int argc, char* argv[]) {
  forwarder_cc::Config config;
  std::string validation_error;
  if (!forwarder_cc::ParseConfig(argc, argv, config, &validation_error)) {
    if (!validation_error.empty()) {
      std::cerr << "forwarder-cc: " << validation_error << "\n";
      return 1;
    }
    return 0;  // --help / --version
  }

  forwarder_cc::InitLogging(config.log_level, config.log_file);

  if (!config.pair_pin.empty()) {
    std::string error_msg;
    std::string ini = forwarder_cc::GetRenderedPairConfig(config.pair_pin, config, error_msg);
    if (ini.empty()) {
      std::cerr << "forwarder-cc: Could not pair with server: " << error_msg << "\n";
      return 1;
    }
    if (config.write_config && !config.config_file_path.empty()) {
      std::ofstream f(config.config_file_path);
      if (!f) {
        std::cerr << "forwarder-cc: Could not write configuration file: " << config.config_file_path
                  << "\n";
        return 1;
      }
      f << ini;
      if (!f) {
        std::cerr << "forwarder-cc: Could not write configuration file\n";
        return 1;
      }
    } else {
      std::cout << ini;
    }
    return 0;
  }

  if (config.connect_host.empty()) {
    std::cerr << "forwarder-cc: --connect-host is required\n";
    return 1;
  }
  if (config.client_id.empty()) {
    std::cerr << "forwarder-cc: --client-id is required\n";
    return 1;
  }
  if (config.client_key.empty()) {
    std::cerr << "forwarder-cc: --client-key is required\n";
    return 1;
  }
  if (config.gateway_id.empty() && !config.server_side) {
    std::cerr << "forwarder-cc: --gateway is required when not server-side\n";
    return 1;
  }
  // Align with client-cc / Go default when unset.
  if (config.endpoint.empty()) {
    config.endpoint = "ingress.eu1.cluster.kudzu.gr:443";
  }

  client_cc::AnalyticsClientConfig client_config;
  client_config.client_id = config.client_id;
  client_config.client_key = config.client_key;
  client_config.endpoint = config.endpoint;
  client_config.ca_file = config.ca_file;
  client_config.ssl_target_name_override = config.ssl_target_name_override;
  client_config.connect_timeout = config.connect_timeout > 0 ? config.connect_timeout : 30;
  client_config.request_timeout = config.request_timeout;
  client_config.max_reconnect_backoff =
      config.max_reconnect_backoff > 0 ? config.max_reconnect_backoff : 60;
  client_config.server_side = config.server_side;

  client_cc::Client client(client_config);

  forwarder_cc::UdpProxyConfig proxy_config;
  proxy_config.listen_host = config.listen_host;
  proxy_config.listen_port_up = config.listen_port_up;
  proxy_config.listen_port_down = config.listen_port_down;
  proxy_config.connect_host = config.connect_host;
  proxy_config.connect_port_up = config.connect_port_up;
  proxy_config.connect_port_down = config.connect_port_down;
  proxy_config.connect_interface = config.connect_interface;
  proxy_config.buffer_size = config.buffer_size;
  proxy_config.max_streams = config.max_udp_streams;
  proxy_config.reconnect_interval_sec = config.connect_retry_interval;
  proxy_config.debug_dump = config.debug_dump;

  forwarder_cc::UdpProxy proxy(proxy_config);
  if (!proxy.Start()) {
    std::cerr << "forwarder-cc: failed to start UDP proxy\n";
    return 1;
  }

  forwarder_cc::AnalyticsForwarder forwarder(config, &client, &proxy);
  forwarder.Run();

  proxy.Stop();
  return 0;
}
