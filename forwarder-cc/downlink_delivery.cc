#include "downlink_delivery.h"

#include <chrono>
#include <cstdio>
#include <openssl/evp.h>

namespace forwarder_cc {
namespace {
std::string Hex(const unsigned char* data, size_t length) {
  const char* digits = "0123456789abcdef";
  std::string result;
  for (size_t i = 0; i < length; ++i) {
    result += digits[data[i] >> 4];
    result += digits[data[i] & 15];
  }
  return result;
}
int64_t NowMs() {
  return std::chrono::duration_cast<std::chrono::milliseconds>(
      std::chrono::system_clock::now().time_since_epoch()).count();
}
std::string DeliveryID(const std::string& eui, const std::string& configured,
                       const SockAddr4& peer, uint16_t token) {
  std::string scope = "peer:" + SockAddrToString(peer);
  if (!configured.empty()) scope = "id:" + configured;
  else if (eui.size() == 8) scope = "eui:" + Hex(reinterpret_cast<const unsigned char*>(eui.data()), 8);
  char suffix[6];
  std::snprintf(suffix, sizeof(suffix), ":%04x", static_cast<unsigned>(token));
  return scope + suffix;
}
}  // namespace

api::AnalyticsDownlinkDelivery CommandDelivery(const SemtechUDPMessage& message,
    const std::string& eui, const std::string& configured, const SockAddr4& peer) {
  api::AnalyticsDownlinkDelivery out;
  unsigned char digest[EVP_MAX_MD_SIZE];
  unsigned int size = 0;
  if (EVP_Digest(message.data.data(), message.data.size(), digest, &size, EVP_sha256(), nullptr) != 1)
    return out;
  out.set_id(DeliveryID(eui, configured, peer, message.token));
  out.set_commandkey(Hex(digest, size));
  out.set_observedunixms(NowMs());
  return out;
}

api::AnalyticsDownlinkOutcome AckOutcome(const SemtechUDPMessage& message,
    const std::string& configured, const SockAddr4& peer) {
  api::AnalyticsDownlinkOutcome out;
  std::string eui(reinterpret_cast<const char*>(message.GatewayEUI()), 8);
  out.set_id(DeliveryID(eui, configured, peer, message.token));
  out.set_observedunixms(NowMs());
  // An empty TX_ACK body explicitly means success in Semtech UDP v2.
  if (message.BodySize() == 0) {
    out.set_status(api::OUTCOME_ACCEPTED);
    return out;
  }
  auto payload = nlohmann::json::parse(message.BodyPtr(), message.BodyPtr() + message.BodySize(), nullptr, false);
  if (!payload.is_object() || !payload.contains("txpk_ack") || !payload["txpk_ack"].is_object() ||
      !payload["txpk_ack"].contains("error") || !payload["txpk_ack"]["error"].is_string()) {
    out.set_reason("ack_invalid");
    return out;
  }
  std::string reason = payload["txpk_ack"]["error"].get<std::string>();
  if (reason.empty() || reason.size() > 256) out.set_reason("ack_invalid");
  else if (reason == "NONE") out.set_status(api::OUTCOME_ACCEPTED);
  else { out.set_status(api::OUTCOME_REJECTED); out.set_reason(reason); }
  return out;
}
}  // namespace forwarder_cc
