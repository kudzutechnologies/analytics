/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#pragma once

#include "AnalyticsPairing.h"
#include "config.h"
#include <string>

namespace basicstation_analytics {

// Fetch pairing credentials and render an INI snippet. Empty on failure.
std::string RenderPairConfig(const Config& current_config,
                             const client_cc::PairingConfig& pair_config);

std::string GetRenderedPairConfig(const std::string& pin,
                                  const Config& current_config,
                                  std::string& error_msg);

}  // namespace basicstation_analytics
