/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
/* Wire protocol for basicstation analytics UNIX DGRAM. Must match basicstation src/analytics.h. */

#ifndef BASICSTATION_ANALYTICS_PROTOCOL_H
#define BASICSTATION_ANALYTICS_PROTOCOL_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#define ANA_MAGIC   0xA7
#define ANA_VERSION 1
/* Host-endian validation marker. Producer and consumer share an architecture. */
#define ANA_ENDIAN  0x01020304u

#define ANA_MAX_PAYLOAD 255

enum {
    ANA_UPLINK   = 0x01,
    ANA_DOWNLINK = 0x02,
    ANA_STAT     = 0x03,
    ANA_HELLO    = 0x04,
};

struct ana_hdr {
    uint8_t  magic;
    uint8_t  version;
    uint8_t  type;
    uint8_t  reserved;
    uint32_t endian;
} __attribute__((packed));

struct ana_hello {
    struct ana_hdr hdr;
    uint64_t       gateway_eui;
    uint8_t        station_ver[8];
} __attribute__((packed));

/* Fixed metadata prefix; LoRaWAN PHYPayload follows as a variable datagram tail. */
struct ana_uplink {
    struct ana_hdr hdr;
    int64_t        rx_wall_us;
    int64_t        rx_gps_time;
    int64_t        xtime;
    uint32_t       freq;
    int16_t        rssi;
    int16_t        snr_x4;
    int32_t        fts;
    uint8_t        dr;
    uint8_t        sf;         /* 0=FSK/unknown, 7-12 */
    uint8_t        bw;         /* 0=125k, 1=250k, 2=500k */
    uint8_t        crc_status;
    uint16_t       size;
} __attribute__((packed));

struct ana_downlink {
    struct ana_hdr hdr;
    int64_t        tx_time_us; /* wall-clock microseconds (UTC) */
    int64_t        tx_gps_time;
    uint32_t       freq;
    int16_t        txpow_x10;
    uint8_t        dr;
    uint8_t        sf;
    uint8_t        bw;
    uint8_t        txflags;
    uint8_t        txunit;
    uint8_t        addcrc;
    uint16_t       preamble;
    uint16_t       size;
} __attribute__((packed));

struct ana_stat {
    struct ana_hdr hdr;
    int64_t        gw_time_us;
    float          latitude;
    float          longitude;
    float          altitude;
    uint32_t       rx_packets;
    uint32_t       rx_ok;
    uint32_t       rx_forwarded;
    uint32_t       tx_received;
    uint32_t       tx_emitted;
    uint8_t        is_gauge;
    uint8_t        reserved[3];
    float          temperature;
} __attribute__((packed));

#define ANA_UPLINK_LEN(sz)   (sizeof(struct ana_uplink) + (size_t)(sz))
#define ANA_DOWNLINK_LEN(sz) (sizeof(struct ana_downlink) + (size_t)(sz))

static inline int ana_hdr_ok(const struct ana_hdr* h) {
    return h && h->magic == ANA_MAGIC && h->version == ANA_VERSION && h->endian == ANA_ENDIAN;
}

#ifdef __cplusplus
}
#endif

#endif
