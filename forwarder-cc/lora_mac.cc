#include "lora_mac.h"

namespace forwarder_cc {

int GetLoRaWANHeaderLen(const unsigned char* data, size_t dlen) {
  if (dlen < 1) return 0;

  unsigned char mhdr = data[0];
  unsigned char mtype = (mhdr & 0xE0) >> 5;

  if (mtype == MTypeJoinReq || mtype == MTypeRejoinReq) {
    if (dlen < 19) return static_cast<int>(dlen);
    return 19;
  }
  if (mtype == MTypeJoinAccept) {
    return static_cast<int>(dlen);
  }
  if (mtype == MTypeProprietary) {
    return static_cast<int>(dlen);
  }

  if (dlen < 8) return static_cast<int>(dlen);

  unsigned char fctrl = data[5];
  int fopts_len = static_cast<int>(fctrl & 0x0F);
  if (dlen < static_cast<size_t>(8 + fopts_len + 1)) {
    return static_cast<int>(dlen);
  }
  return 8 + fopts_len + 1;
}

}  // namespace forwarder_cc
