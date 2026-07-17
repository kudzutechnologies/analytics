# client-cc

C++ gRPC Client for pushing analytics data to Kudzu Analytics platform.

## Overview

This package implements a gRPC analytics client in C++ that can be used for embedding analytics forwarders to the Kudzu Analytics platform. It uses protobuf and gRPC, and expects the generated protobuf and gRPC stubs to be available in `src/protobuf`.

It also provides a shared **pairing** API (`AnalyticsPairing.h`) used by forwarders to download edge credentials over HTTPS.

## Dependencies
- gRPC (C++)
- Protobuf (C++)
- OpenSSL (SHA256, TLS root loading, pairing HTTPS)
- nlohmann/json (fetched by CMake via FetchContent for pairing JSON parsing)

## TLS trust

Both gRPC and pairing HTTPS use the same additive trust rules:

- No `ca_file`: system / platform default roots
- With `ca_file`: system roots **plus** the PEM certificates from that file (the file must load successfully)

## Usage Example

```cpp
#include "Client.h"
#include "protobuf/analytics.pb.h"

int main() {
    client_cc::AnalyticsClientConfig config;
    config.client_id = "1122334455667788";
    config.client_key = "11223344556677889900aabbccddeeff";
    // Optionally set endpoint, ca_file, etc.

    client_cc::Client client(config);
    if (!client.Connect()) {
        throw std::runtime_error("Failed to connect");
    }

    api::AnalyticsMetrics metrics;
    // Fill metrics fields as needed
    if (!client.PushMetrics(metrics)) {
        throw std::runtime_error("Failed to push metrics");
    }

    client.Disconnect();
    return 0;
}
```

## Pairing Example

```cpp
#include "AnalyticsPairing.h"

client_cc::PairingOptions opts;
opts.pin = "123-456";
opts.ca_file = "/path/to/extra-ca.pem";  // optional

client_cc::PairingConfig cfg;
std::string error;
if (!client_cc::FetchPairingConfig(opts, cfg, error)) {
    throw std::runtime_error(error);
}
// cfg.client_id, cfg.client_key, cfg.gateway_id, cfg.extras
```

Default pairing base URL: `https://eu1.cluster.kudzu.gr/api/v1/pairing/edge`.

## API
- `AnalyticsClientConfig`: Configuration struct for the client.
- `Client`: Main class with methods:
  - `Connect()`: Connect to the analytics server.
  - `Disconnect()`: Disconnect from the server.
  - `PushMetrics(const api::AnalyticsMetrics&)`: Push analytics metrics.
- `FetchPairingConfig` / `PairingConfig` / `PairingOptions`: Edge pairing over HTTPS.

## Build / Tests

```bash
mkdir build && cd build
cmake -DCLIENT_CC_BUILD_TESTS=ON ..
cmake --build .
ctest --output-on-failure
```

## Notes
- Requires gRPC and protobuf C++ libraries.
- Protobuf and gRPC stubs must be generated and available in `src/protobuf`.
