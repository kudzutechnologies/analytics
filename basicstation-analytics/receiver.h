/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#pragma once

#include <cstdint>
#include <string>
#include <sys/types.h>

namespace api {
class AnalyticsMetrics;
}

namespace basicstation_analytics {

struct PollStats {
    int parsed = 0;
    int rejected = 0;
};

class Receiver {
public:
    Receiver() = default;
    ~Receiver();

    // err may be null. Returns false on bind/fcntl failure.
    bool Open(const std::string& socket_path, mode_t mode = 0660, std::string* err = nullptr);
    void Close();
    bool IsOpen() const { return fd_ >= 0; }
    int Fd() const { return fd_; }

    // Optional Kudzu ObjectID (from config/pairing). Never synthesized from EUI.
    void SetGatewayId(const std::string& gateway_id) { gateway_id_ = gateway_id; }

    // Non-blocking: read available datagrams and append to metrics.
    PollStats Poll(api::AnalyticsMetrics* metrics);

    // Parse a single native-layout datagram. Returns number of metrics messages appended
    // (0 for HELLO / rejected, 1 for uplink/downlink/stat).
    int ParseDatagram(const char* buf, size_t n, api::AnalyticsMetrics* metrics);

    const std::string& GatewayId() const { return gateway_id_; }
    const std::string& GatewayEuiHex() const { return gateway_eui_hex_; }
    const std::string& GatewayEuiBytes() const { return gateway_eui_bytes_; }
    bool HasGatewayEui() const { return !gateway_eui_bytes_.empty(); }
    // Ready to publish once ObjectID and EUI are known.
    bool HasIdentity() const { return HasGatewayEui() && !gateway_id_.empty(); }

    uint64_t Rejected() const { return rejected_; }

private:
    int fd_ = -1;
    bool bound_path_owned_ = false;
    std::string socket_path_;
    mode_t socket_mode_ = 0660;
    std::string gateway_id_;
    std::string gateway_eui_hex_;
    std::string gateway_eui_bytes_;
    uint64_t gateway_eui_ = 0;
    uint64_t rejected_ = 0;

    void ApplyGatewayFields(api::AnalyticsMetrics* metrics) const;
};

}  // namespace basicstation_analytics
