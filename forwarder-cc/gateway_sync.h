#pragma once

#include "config.h"
#include "protobuf/api.pb.h"

#include <cstdint>
#include <string>
#include <vector>

namespace forwarder_cc {

struct ParsedGatewaySync {
  std::string gateway_id;
  std::string external_id;
  std::vector<uint8_t> eui;
  std::string name;
  api::GatewayLocation location;
  bool has_location = false;
  std::vector<api::GatewayRadio> radios;
  bool has_radios = false;
  bool has_any = false;
};

// Validates and parses gateway-* sync options.
// Returns false with empty error when no sync options are present (parsed.has_any=false).
// Returns false with a non-empty error on validation failure.
// Returns true when sync is enabled and valid.
bool ParseGatewaySyncConfig(const Config& c, ParsedGatewaySync& parsed, std::string& error);

bool ParseGatewayEUI(const std::string& raw, std::vector<uint8_t>& out, std::string& error);
bool ParseGatewayLocation(const std::string& raw, api::GatewayLocation& out, std::string& error);
bool ParseGatewayRadios(const std::string& raw, std::vector<api::GatewayRadio>& out, std::string& error);

api::ReqGatewayUpsert ToUpsertRequest(const ParsedGatewaySync& parsed);

}  // namespace forwarder_cc
