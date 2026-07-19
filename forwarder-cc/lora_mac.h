#pragma once

#include <cstddef>

namespace forwarder_cc {

// LoRaWAN MAC header types (MHDR MType)
constexpr unsigned char MTypeJoinReq = 0x00;
constexpr unsigned char MTypeJoinAccept = 0x01;
constexpr unsigned char MTypeUnconfirmedUp = 0x02;
constexpr unsigned char MTypeUnconfirmedDown = 0x03;
constexpr unsigned char MTypeConfirmedUp = 0x04;
constexpr unsigned char MTypeConfirmedDown = 0x05;
constexpr unsigned char MTypeRejoinReq = 0x06;
constexpr unsigned char MTypeProprietary = 0x07;

// Returns length of LoRaWAN header (fhdr) for payload. Used to set fhdr and remainder for uniqueId.
int GetLoRaWANHeaderLen(const unsigned char* data, size_t dlen);

}  // namespace forwarder_cc
