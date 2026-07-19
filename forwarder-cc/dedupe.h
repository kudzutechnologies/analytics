#pragma once

#include <cstddef>

namespace api {
class AnalyticsUplink;
class AnalyticsDownlink;
}

namespace forwarder_cc {

// Compute unique ID for uplink (same algorithm as api/dedupe.go).
void ComputeUniqueIdUp(api::AnalyticsUplink* up, const unsigned char* full_payload, size_t len);

// Compute unique ID for downlink.
void ComputeUniqueIdDown(api::AnalyticsDownlink* down, const unsigned char* full_payload, size_t len);

}  // namespace forwarder_cc
