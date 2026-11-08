/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "config.h"
#include "tests/test_harness.h"

#include <cstdio>
#include <cstdlib>
#include <fstream>

using namespace basicstation_analytics;
using namespace bs_analytics_test;

int main() {
  {
    Config config;
    ExpectEq(config.endpoint, "ingress.eu1.cluster.kudzu.gr:443",
             "default analytics endpoint");
    ExpectEq(config.pairing_endpoint, "https://console.eu1.cluster.kudzu.gr",
             "default pairing endpoint");
  }

  {
    Config config;
    std::string error;
    char arg0[] = "basicstation-analytics";
    char pair_pin[] = "--pair-pin=123456";
    char analytics_endpoint[] = "--endpoint=analytics.example.com:50051";
    char pairing_endpoint[] = "--pairing-endpoint=https://console.example.com/";
    char* argv[] = {arg0, pair_pin, analytics_endpoint, pairing_endpoint};
    Expect(ParseConfig(4, argv, config, &error), "pairing endpoint flags");
    ExpectEq(config.endpoint, "analytics.example.com:50051",
             "custom analytics endpoint");
    ExpectEq(config.pairing_endpoint, "https://console.example.com/",
             "custom pairing endpoint");
  }

  {
    const char* path = "/tmp/basicstation-analytics-endpoint-test.conf";
    {
      std::ofstream ini(path);
      ini << "analytics-endpoint=file.example.com:50051\n"
          << "pairing-endpoint=https://file-console.example.com/\n";
    }
    setenv("ANALYTICS_ENDPOINT", "env.example.com:50051", 1);
    setenv("PAIRING_ENDPOINT", "https://env-console.example.com/", 1);
    Config env_config;
    std::string env_error;
    char env_arg0[] = "basicstation-analytics";
    char env_config_arg[] =
        "--config=/tmp/basicstation-analytics-endpoint-test.conf";
    char env_pair_pin[] = "--pair-pin=123456";
    char* env_argv[] = {env_arg0, env_config_arg, env_pair_pin};
    Expect(ParseConfig(3, env_argv, env_config, &env_error),
           "environment endpoint precedence");
    ExpectEq(env_config.endpoint, "env.example.com:50051",
             "environment analytics wins over file");
    ExpectEq(env_config.pairing_endpoint, "https://env-console.example.com/",
             "environment pairing wins over file");

    Config config;
    std::string error;
    char arg0[] = "basicstation-analytics";
    char config_arg[] = "--config=/tmp/basicstation-analytics-endpoint-test.conf";
    char pair_pin[] = "--pair-pin=123456";
    char analytics_endpoint[] =
        "--analytics-endpoint=ingress.eu1.cluster.kudzu.gr:443";
    char pairing_endpoint[] =
        "--pairing-endpoint=https://console.eu1.cluster.kudzu.gr/";
    char* argv[] = {arg0, config_arg, pair_pin, analytics_endpoint, pairing_endpoint};
    Expect(ParseConfig(5, argv, config, &error), "CLI endpoint precedence");
    ExpectEq(config.endpoint, "ingress.eu1.cluster.kudzu.gr:443",
             "CLI analytics default overrides environment");
    ExpectEq(config.pairing_endpoint, "https://console.eu1.cluster.kudzu.gr/",
             "CLI pairing default overrides environment");
    unsetenv("ANALYTICS_ENDPOINT");
    unsetenv("PAIRING_ENDPOINT");
    std::remove(path);
  }

  {
    Config config;
    std::string error;
    char arg0[] = "basicstation-analytics";
    char socket_opt[] = "--socket=/tmp/analytics.sock";
    char client_id[] = "--client-id=0011";
    char client_key[] = "--client-key=2233";
    char* argv[] = {arg0, socket_opt, client_id, client_key};

    Expect(!ParseConfig(4, argv, config, &error),
           "gateway ObjectID is required");
    Expect(error.find("--gateway") != std::string::npos,
           "missing gateway error is reported");
  }

  return Summary();
}
