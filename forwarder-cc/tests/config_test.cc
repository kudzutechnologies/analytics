#include "config.h"
#include "tests/test_harness.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <vector>

using namespace forwarder_cc;
using namespace forwarder_cc_test;

int main() {
  {
    Config c;
    ExpectEq(c.endpoint, "ingress.eu1.cluster.kudzu.gr:443", "default analytics endpoint");
    ExpectEq(c.pairing_endpoint, "https://console.eu1.cluster.kudzu.gr",
             "default pairing endpoint");
  }

  {
    Config c;
    std::string err;
    std::vector<char*> argv = {
        const_cast<char*>("forwarder-cc"),
        const_cast<char*>("--pair-pin=123456"),
        const_cast<char*>("--analytics-endpoint=analytics.example.com:50051"),
        const_cast<char*>("--pairing-endpoint=https://console.example.com/")};
    Expect(ParseConfig(static_cast<int>(argv.size()), argv.data(), c, &err),
           "pairing endpoint flags");
    ExpectEq(c.endpoint, "analytics.example.com:50051", "custom analytics endpoint");
    ExpectEq(c.pairing_endpoint, "https://console.example.com/",
             "custom pairing endpoint");
  }

  {
    const char* path = "/tmp/forwarder-cc-endpoint-test.conf";
    {
      std::ofstream ini(path);
      ini << "analytics-endpoint=file.example.com:50051\n"
          << "pairing-endpoint=https://file-console.example.com/\n";
    }
    setenv("ANALYTICS_ENDPOINT", "env.example.com:50051", 1);
    setenv("PAIRING_ENDPOINT", "https://env-console.example.com/", 1);
    Config env_config;
    std::string env_err;
    std::vector<char*> env_argv = {
        const_cast<char*>("forwarder-cc"),
        const_cast<char*>("--config=/tmp/forwarder-cc-endpoint-test.conf"),
        const_cast<char*>("--pair-pin=123456")};
    Expect(ParseConfig(static_cast<int>(env_argv.size()), env_argv.data(), env_config,
                       &env_err),
           "environment endpoint precedence");
    ExpectEq(env_config.endpoint, "env.example.com:50051",
             "environment analytics wins over file");
    ExpectEq(env_config.pairing_endpoint, "https://env-console.example.com/",
             "environment pairing wins over file");

    Config c;
    std::string err;
    std::vector<char*> argv = {
        const_cast<char*>("forwarder-cc"),
        const_cast<char*>("--config=/tmp/forwarder-cc-endpoint-test.conf"),
        const_cast<char*>("--pair-pin=123456"),
        const_cast<char*>("--analytics-endpoint=cli.example.com:50051"),
        const_cast<char*>("--pairing-endpoint=https://cli-console.example.com/")};
    Expect(ParseConfig(static_cast<int>(argv.size()), argv.data(), c, &err),
           "endpoint precedence");
    ExpectEq(c.endpoint, "cli.example.com:50051", "CLI analytics wins");
    ExpectEq(c.pairing_endpoint, "https://cli-console.example.com/",
             "CLI pairing wins");
    unsetenv("ANALYTICS_ENDPOINT");
    unsetenv("PAIRING_ENDPOINT");
    std::remove(path);
  }

  {
    Config c;
    std::string err;
    std::vector<char*> argv = {const_cast<char*>("forwarder-cc"),
                               const_cast<char*>("--version")};
    Expect(!ParseConfig(static_cast<int>(argv.size()), argv.data(), c, &err), "version exits");
    Expect(err.empty(), "version no validation error");
  }

  {
    Config c;
    std::string err;
    std::vector<char*> argv = {const_cast<char*>("forwarder-cc"),
                               const_cast<char*>("--gateway-name=only"),
                               const_cast<char*>("--connect-host=x"),
                               const_cast<char*>("--client-id=y"),
                               const_cast<char*>("--client-key=z"),
                               const_cast<char*>("--gateway=g")};
    Expect(!ParseConfig(static_cast<int>(argv.size()), argv.data(), c, &err),
           "invalid sync rejected");
    Expect(!err.empty(), "validation error set");
  }

  {
    Config c;
    std::string err;
    std::vector<char*> argv = {
        const_cast<char*>("forwarder-cc"),
        const_cast<char*>("--gateway-eid=edge-1"),
        const_cast<char*>("--gateway-name=Roof"),
        const_cast<char*>("--connect-host=x"),
        const_cast<char*>("--server-side=true"),
    };
    Expect(ParseConfig(static_cast<int>(argv.size()), argv.data(), c, &err), "valid sync");
    Expect(err.empty(), "no err");
    ExpectEq(c.gateway_eid, "edge-1", "eid");
    ExpectEq(c.gateway_name, "Roof", "name");
    Expect(c.server_side, "server-side");
    ExpectEq(c.max_udp_streams, 256, "server max streams default");
    ExpectEq(c.flush_interval, 5, "server flush default");
    ExpectEq(c.connect_retry_interval, 1, "retry default");
  }

  {
    Config c;
    std::string err;
    std::vector<char*> argv = {
        const_cast<char*>("forwarder-cc"),
        const_cast<char*>("--log-file=/tmp/fwd.log"),
        const_cast<char*>("--log-level=debug"),
        const_cast<char*>("--connect-retry-interval=3"),
        const_cast<char*>("--debug-dump=/tmp/dump.b64"),
    };
    Expect(ParseConfig(static_cast<int>(argv.size()), argv.data(), c, &err), "ops flags");
    ExpectEq(c.log_file, "/tmp/fwd.log", "log-file");
    ExpectEq(c.log_level, "debug", "log-level");
    ExpectEq(c.connect_retry_interval, 3, "retry");
    ExpectEq(c.debug_dump, "/tmp/dump.b64", "debug-dump");
    ExpectEq(c.max_udp_streams, 2, "client max streams");
    ExpectEq(c.flush_interval, 10, "client flush");
  }

  return Summary();
}
