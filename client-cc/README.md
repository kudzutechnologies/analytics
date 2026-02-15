# client-cc

C++ gRPC Client for pushing analytics data to Kudzu Analytics platform.

## Overview

This package implements a gRPC analytics client in C++ that can be used for embedding analytics forwarders to the Kudzu Analytics platform. It uses protobuf and gRPC, and expects the generated protobuf and gRPC stubs to be available in `include/protobuf`.

## Dependencies
- gRPC (C++)
- Protobuf (C++)
- OpenSSL (for SHA256)

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

## API
- `AnalyticsClientConfig`: Configuration struct for the client.
- `Client`: Main class with methods:
  - `Connect()`: Connect to the analytics server.
  - `Disconnect()`: Disconnect from the server.
  - `PushMetrics(const api::AnalyticsMetrics&)`: Push analytics metrics.

## Notes
- Requires gRPC and protobuf C++ libraries.
- Protobuf and gRPC stubs must be generated and available in `include/protobuf`. 