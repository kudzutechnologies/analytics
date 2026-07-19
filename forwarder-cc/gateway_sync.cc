#include "gateway_sync.h"

#include <cctype>
#include <cstdlib>
#include <sstream>
#include <unordered_set>

namespace forwarder_cc {
namespace {

std::string Trim(const std::string& s) {
  size_t start = s.find_first_not_of(" \t\r\n");
  if (start == std::string::npos) return "";
  size_t end = s.find_last_not_of(" \t\r\n");
  return s.substr(start, end == std::string::npos ? std::string::npos : end - start + 1);
}

bool HasGatewaySyncOptions(const Config& c) {
  return !c.gateway_location.empty() || !c.gateway_eui.empty() || !c.gateway_eid.empty() ||
         !c.gateway_name.empty() || !c.gateway_radios.empty();
}

int HexVal(char c) {
  if (c >= '0' && c <= '9') return c - '0';
  if (c >= 'a' && c <= 'f') return c - 'a' + 10;
  if (c >= 'A' && c <= 'F') return c - 'A' + 10;
  return -1;
}

}  // namespace

bool ParseGatewayEUI(const std::string& raw, std::vector<uint8_t>& out, std::string& error) {
  std::string s = Trim(raw);
  std::string hex;
  hex.reserve(s.size());
  for (char c : s) {
    if (c == '-' || c == ':') continue;
    hex.push_back(static_cast<char>(std::tolower(static_cast<unsigned char>(c))));
  }
  if (hex.size() != 16) {
    error = "gateway-eui must be 16 hex characters (8 bytes), got " + std::to_string(hex.size()) +
            " chars";
    return false;
  }
  out.resize(8);
  for (size_t i = 0; i < 8; ++i) {
    int hi = HexVal(hex[i * 2]);
    int lo = HexVal(hex[i * 2 + 1]);
    if (hi < 0 || lo < 0) {
      error = "gateway-eui: invalid hex";
      return false;
    }
    out[i] = static_cast<uint8_t>((hi << 4) | lo);
  }
  return true;
}

bool ParseGatewayLocation(const std::string& raw, api::GatewayLocation& out, std::string& error) {
  std::string s = Trim(raw);
  std::vector<std::string> parts;
  {
    std::stringstream ss(s);
    std::string item;
    while (std::getline(ss, item, ',')) parts.push_back(Trim(item));
  }
  if (parts.size() != 2 && parts.size() != 3) {
    error = "gateway-location must be lat,lon or lat,lon,alt";
    return false;
  }
  char* end = nullptr;
  double lat = std::strtod(parts[0].c_str(), &end);
  if (!end || *end != '\0') {
    error = "gateway-location latitude: invalid number";
    return false;
  }
  double lon = std::strtod(parts[1].c_str(), &end);
  if (!end || *end != '\0') {
    error = "gateway-location longitude: invalid number";
    return false;
  }
  if (lat < -90.0 || lat > 90.0) {
    error = "gateway-location latitude out of range: " + parts[0];
    return false;
  }
  if (lon < -180.0 || lon > 180.0) {
    error = "gateway-location longitude out of range: " + parts[1];
    return false;
  }
  out.set_latitude(lat);
  out.set_longitude(lon);
  if (parts.size() == 3) {
    double alt = std::strtod(parts[2].c_str(), &end);
    if (!end || *end != '\0') {
      error = "gateway-location altitude: invalid number";
      return false;
    }
    out.set_altitude(alt);
  }
  return true;
}

bool ParseGatewayRadios(const std::string& raw,
                        std::vector<api::GatewayRadio>& out,
                        std::string& error) {
  out.clear();
  std::unordered_set<int32_t> seen;
  std::stringstream ss(raw);
  std::string entry;
  while (std::getline(ss, entry, ',')) {
    entry = Trim(entry);
    if (entry.empty()) continue;
    std::vector<std::string> parts;
    {
      std::stringstream es(entry);
      std::string p;
      while (std::getline(es, p, ':')) parts.push_back(Trim(p));
    }
    if (parts.size() != 2 && parts.size() != 3) {
      error = "gateway-radios entry \"" + entry +
              "\" must be rf_chain_id:max_tx_power[:tx_sensitivity]";
      return false;
    }
    char* end = nullptr;
    long chain = std::strtol(parts[0].c_str(), &end, 10);
    if (!end || *end != '\0') {
      error = "gateway-radios rf_chain_id: invalid number";
      return false;
    }
    float power = static_cast<float>(std::strtod(parts[1].c_str(), &end));
    if (!end || *end != '\0') {
      error = "gateway-radios max_tx_power: invalid number";
      return false;
    }
    int32_t rf = static_cast<int32_t>(chain);
    if (seen.count(rf)) {
      error = "gateway-radios duplicate rf_chain_id " + std::to_string(rf);
      return false;
    }
    seen.insert(rf);
    api::GatewayRadio radio;
    radio.set_rf_chain(rf);
    radio.set_max_tx_power(power);
    if (parts.size() == 3) {
      float sens = static_cast<float>(std::strtod(parts[2].c_str(), &end));
      if (!end || *end != '\0') {
        error = "gateway-radios tx_sensitivity: invalid number";
        return false;
      }
      radio.set_gain_sensitivity(sens);
    }
    out.push_back(std::move(radio));
  }
  if (out.empty()) {
    error = "gateway-radios is empty";
    return false;
  }
  return true;
}

bool ParseGatewaySyncConfig(const Config& c, ParsedGatewaySync& parsed, std::string& error) {
  parsed = ParsedGatewaySync{};
  error.clear();
  if (!HasGatewaySyncOptions(c)) {
    return false;  // no sync options; not an error
  }

  parsed.has_any = true;
  parsed.gateway_id = Trim(c.gateway_id);
  parsed.external_id = Trim(c.gateway_eid);
  parsed.name = Trim(c.gateway_name);

  if (!c.gateway_eui.empty()) {
    if (!ParseGatewayEUI(c.gateway_eui, parsed.eui, error)) return false;
  }
  if (!c.gateway_location.empty()) {
    if (!ParseGatewayLocation(c.gateway_location, parsed.location, error)) return false;
    parsed.has_location = true;
  }
  if (!c.gateway_radios.empty()) {
    if (!ParseGatewayRadios(c.gateway_radios, parsed.radios, error)) return false;
    parsed.has_radios = true;
  }
  if (parsed.external_id.empty() && parsed.eui.empty()) {
    error =
        "gateway sync requires gateway-eid or gateway-eui when any gateway-* option is set";
    return false;
  }
  return true;
}

api::ReqGatewayUpsert ToUpsertRequest(const ParsedGatewaySync& parsed) {
  api::ReqGatewayUpsert req;
  if (!parsed.gateway_id.empty()) req.set_gateway_id(parsed.gateway_id);
  if (!parsed.external_id.empty()) req.set_external_id(parsed.external_id);
  if (!parsed.eui.empty()) {
    req.set_gateway_eui(parsed.eui.data(), parsed.eui.size());
  }
  if (!parsed.name.empty()) req.set_name(parsed.name);
  if (parsed.has_location) {
    *req.mutable_location() = parsed.location;
  }
  if (parsed.has_radios) {
    auto* radios = req.mutable_radios();
    for (const auto& r : parsed.radios) {
      *radios->add_values() = r;
    }
  }
  return req;
}

}  // namespace forwarder_cc
