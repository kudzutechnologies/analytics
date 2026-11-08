/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#pragma once

#include <cstddef>

namespace api {
class AnalyticsUplink;
class AnalyticsDownlink;
} // namespace api

namespace basicstation_analytics {

void ComputeUniqueIdUp(api::AnalyticsUplink *up,
                       const unsigned char *full_payload, size_t len);
void ComputeUniqueIdDown(api::AnalyticsDownlink *down,
                         const unsigned char *full_payload, size_t len);

} // namespace basicstation_analytics
