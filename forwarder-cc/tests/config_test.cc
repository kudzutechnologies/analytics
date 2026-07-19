#include "config.h"
#include "tests/test_harness.h"

#include <cstring>
#include <vector>

using namespace forwarder_cc;
using namespace forwarder_cc_test;

int main() {
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
