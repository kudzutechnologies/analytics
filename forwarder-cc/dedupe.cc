#include "dedupe.h"
#include "protobuf/analytics.pb.h"
#if defined(__GNUC__) || defined(__clang__)
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#endif
#include <openssl/sha.h>
#if defined(__GNUC__) || defined(__clang__)
#pragma GCC diagnostic pop
#endif
#include <cstring>

namespace forwarder_cc {

namespace {

void PutUint32LE(unsigned char* p, uint32_t v) {
  p[0] = static_cast<unsigned char>(v);
  p[1] = static_cast<unsigned char>(v >> 8);
  p[2] = static_cast<unsigned char>(v >> 16);
  p[3] = static_cast<unsigned char>(v >> 24);
}

void PutUint16LE(unsigned char* p, uint16_t v) {
  p[0] = static_cast<unsigned char>(v);
  p[1] = static_cast<unsigned char>(v >> 8);
}

}  // namespace

void ComputeUniqueIdUp(api::AnalyticsUplink* up, const unsigned char* full_payload, size_t len) {
  unsigned char extra[6 + 5];
  size_t extra_len = 6;
  extra[0] = 0;
  extra[1] = 0;
  extra[2] = 0;
  extra[3] = 0;
  extra[4] = 0;
  extra[5] = 0;
  PutUint32LE(extra + 0, static_cast<uint32_t>(up->frequency()));
  PutUint16LE(extra + 4, static_cast<uint16_t>(up->codingrate()));

  if (up->has_dataratelora()) {
    const auto* lora = &up->dataratelora();
    extra[6] = 1;
    extra[7] = 0;
    extra[8] = 0;
    extra[9] = 0;
    extra[10] = 0;
    PutUint16LE(extra + 7, static_cast<uint16_t>(lora->bandwidth()));
    PutUint16LE(extra + 9, static_cast<uint16_t>(lora->spreadingfactor()));
    extra_len = 11;
  } else if (up->has_dataratefsk()) {
    extra[6] = 2;
    extra[7] = 0;
    extra[8] = 0;
    extra[9] = 0;
    extra[10] = 0;
    PutUint32LE(extra + 7, up->dataratefsk());
    extra_len = 11;
  }

  unsigned char hash[SHA_DIGEST_LENGTH];
  SHA_CTX ctx;
  SHA1_Init(&ctx);
  SHA1_Update(&ctx, full_payload, len);
  SHA1_Update(&ctx, extra, extra_len);
  SHA1_Final(hash, &ctx);

  up->set_uniqueid(hash, SHA_DIGEST_LENGTH);
}

void ComputeUniqueIdDown(api::AnalyticsDownlink* down, const unsigned char* full_payload, size_t len) {
  unsigned char extra[6 + 5];
  size_t extra_len = 6;
  extra[0] = 0;
  extra[1] = 0;
  extra[2] = 0;
  extra[3] = 0;
  extra[4] = 0;
  extra[5] = 0;
  PutUint32LE(extra + 0, static_cast<uint32_t>(down->frequency()));
  PutUint16LE(extra + 4, static_cast<uint16_t>(down->codingrate()));

  if (down->has_dataratelora()) {
    const auto* lora = &down->dataratelora();
    extra[6] = 1;
    extra[7] = 0;
    extra[8] = 0;
    extra[9] = 0;
    extra[10] = 0;
    PutUint16LE(extra + 7, static_cast<uint16_t>(lora->bandwidth()));
    PutUint16LE(extra + 9, static_cast<uint16_t>(lora->spreadingfactor()));
    extra_len = 11;
  } else if (down->has_dataratefsk()) {
    extra[6] = 2;
    extra[7] = 0;
    extra[8] = 0;
    extra[9] = 0;
    extra[10] = 0;
    PutUint32LE(extra + 7, down->dataratefsk());
    extra_len = 11;
  }

  unsigned char hash[SHA_DIGEST_LENGTH];
  SHA_CTX ctx;
  SHA1_Init(&ctx);
  SHA1_Update(&ctx, full_payload, len);
  SHA1_Update(&ctx, extra, extra_len);
  SHA1_Final(hash, &ctx);

  down->set_uniqueid(hash, SHA_DIGEST_LENGTH);
}

}  // namespace forwarder_cc
