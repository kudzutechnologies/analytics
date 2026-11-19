#pragma once

#include "semtech_udp.h"
#include "udp_proxy.h"
#include "protobuf/analytics.pb.h"

namespace forwarder_cc {

api::AnalyticsDownlinkDelivery CommandDelivery(const SemtechUDPMessage& message,
    const std::string& gateway_eui, const std::string& configured, const SockAddr4& peer);
api::AnalyticsDownlinkOutcome AckOutcome(const SemtechUDPMessage& message,
    const std::string& configured, const SockAddr4& peer);

}  // namespace forwarder_cc
