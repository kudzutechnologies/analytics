/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "receiver.h"
#include "protocol.h"
#include "dedupe.h"
#include "protobuf/analytics.pb.h"

#include <algorithm>
#include <cerrno>
#include <cstdio>
#include <cstring>
#include <fcntl.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>

namespace basicstation_analytics {

namespace {

constexpr size_t kMaxDatagram = sizeof(struct ana_uplink) + ANA_MAX_PAYLOAD + 64;


api::LoRaSF WireSfToProto(uint8_t sf) {
    if (sf >= 7 && sf <= 12) {
        // Proto: SF12=1 .. SF7=6
        return static_cast<api::LoRaSF>(13 - sf);
    }
    return api::SF_UNKNOWN;
}

api::LoRaBW WireBwToProto(uint8_t bw) {
    switch (bw) {
        case 0: return api::BW_125k;
        case 1: return api::BW_250k;
        case 2: return api::BW_500k;
        default: return api::BW_UNKNOWN;
    }
}

api::CRCStatus WireCrcToProto(uint8_t c) {
    if (c <= 2) return static_cast<api::CRCStatus>(c);
    return api::MISSING;
}

std::string EuiToHex(uint64_t eui) {
    char buf[24];
    snprintf(buf, sizeof(buf), "%016llx", static_cast<unsigned long long>(eui));
    return std::string(buf);
}

std::string EuiToBytes(uint64_t eui) {
    std::string s;
    s.resize(8);
    for (int i = 0; i < 8; i++) {
        s[7 - i] = static_cast<char>((eui >> (i * 8)) & 0xff);
    }
    return s;
}

void FillUplink(const struct ana_uplink* w, const uint8_t* payload, api::AnalyticsUplink* p) {
    p->set_rxwalltime(w->rx_wall_us);
    p->set_rxgpstime(w->rx_gps_time);
    p->set_rxfinishedtime(w->xtime);
    p->set_frequency(w->freq / 1e6f);
    p->set_rfchain(0);
    p->set_crc(WireCrcToProto(w->crc_status));
    p->set_size(w->size);
    const size_t fhdr_n = std::min<size_t>(16, w->size);
    if (payload && fhdr_n > 0) {
        p->set_fhdr(payload, fhdr_n);
    }
    if (w->sf == 0) {
        p->set_modulation(api::FSK);
        p->set_dataratefsk(w->dr);
        p->set_codingrate(api::CR_UNKNOWN);
    } else {
        p->set_modulation(api::LORA);
        p->set_codingrate(api::CR_UNKNOWN);
        auto* dr = p->mutable_dataratelora();
        dr->set_spreadingfactor(WireSfToProto(w->sf));
        dr->set_bandwidth(WireBwToProto(w->bw));
    }
    auto* ant = p->add_ant();
    ant->set_antenna(0);
    ant->set_ifchan(0);
    ant->set_rssic(w->rssi);
    ant->set_lsnr(w->snr_x4 / 4.0f);
    ant->set_ftime(w->fts);
    if (payload && w->size > 0) {
        ComputeUniqueIdUp(p, payload, w->size);
    }
}

void FillDownlink(const struct ana_downlink* w, const uint8_t* payload, api::AnalyticsDownlink* p) {
    p->set_txtime(w->tx_time_us);
    p->set_txgpstime(w->tx_gps_time);
    p->set_frequency(w->freq / 1e6f);
    p->set_rfchain(w->txunit);
    p->set_power(w->txpow_x10 / 10.0f);
    p->set_size(w->size);
    const size_t fhdr_n = std::min<size_t>(16, w->size);
    if (payload && fhdr_n > 0) {
        p->set_fhdr(payload, fhdr_n);
    }
    // Class A / Class C / ping use inverted IQ except beacons (matches ral_lgw).
    p->set_invertpolarity((w->txflags & 0x20) == 0);  // TXFLAG_BCN = 0x20
    p->set_immediately((w->txflags & 0x10) != 0);     // TXFLAG_CLSC approx
    p->set_rfpreamble(w->preamble);
    p->set_nocrc(w->addcrc == 0);
    p->set_rxwalltime(w->tx_time_us / 1000);  // ms wall clock approx
    if (w->sf == 0) {
        p->set_modulation(api::FSK);
        p->set_dataratefsk(w->dr);
        p->set_codingrate(api::CR_UNKNOWN);
    } else {
        p->set_modulation(api::LORA);
        p->set_codingrate(api::CR_UNKNOWN);
        auto* dr = p->mutable_dataratelora();
        dr->set_spreadingfactor(WireSfToProto(w->sf));
        dr->set_bandwidth(WireBwToProto(w->bw));
    }
    if (payload && w->size > 0) {
        ComputeUniqueIdDown(p, payload, w->size);
    }
}

void FillStat(const struct ana_stat* w, api::AnalyticsStat* p) {
    p->set_gwtime(w->gw_time_us / 1000);  // wire us → API ms
    p->set_gwlatitude(w->latitude);
    p->set_gwlongitude(w->longitude);
    p->set_gwaltitude(w->altitude);
    p->set_rxpackets(w->rx_packets);
    p->set_rxwithvalidphycrc(w->rx_ok);
    p->set_rxforwarded(w->rx_forwarded);
    p->set_txreceived(w->tx_received);
    p->set_txemitted(w->tx_emitted);
    p->set_isgauge(w->is_gauge != 0);
    if (w->temperature == w->temperature) {
        p->set_gwtemp(w->temperature);
    }
}

}  // namespace

Receiver::~Receiver() { Close(); }

bool Receiver::Open(const std::string& socket_path, mode_t mode, std::string* err) {
    if (fd_ >= 0) return true;
    int fd = socket(AF_UNIX, SOCK_DGRAM, 0);
    if (fd < 0) {
        if (err) *err = std::string("socket: ") + strerror(errno);
        return false;
    }
    struct sockaddr_un addr = {};
    addr.sun_family = AF_UNIX;
    if (socket_path.size() >= sizeof(addr.sun_path)) {
        close(fd);
        if (err) *err = "socket path too long";
        return false;
    }
    std::memcpy(addr.sun_path, socket_path.c_str(), socket_path.size() + 1);

    struct stat st;
    if (stat(socket_path.c_str(), &st) == 0) {
        if (!S_ISSOCK(st.st_mode)) {
            close(fd);
            if (err) *err = "path exists and is not a socket";
            return false;
        }
        unlink(socket_path.c_str());
    }

    if (bind(fd, reinterpret_cast<struct sockaddr*>(&addr), sizeof(addr)) != 0) {
        if (err) *err = std::string("bind: ") + strerror(errno);
        close(fd);
        return false;
    }
    chmod(socket_path.c_str(), mode);
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0 || fcntl(fd, F_SETFL, flags | O_NONBLOCK) != 0) {
        if (err) *err = std::string("fcntl: ") + strerror(errno);
        unlink(socket_path.c_str());
        close(fd);
        return false;
    }
    int rcv = receive_buffer_;
    setsockopt(fd, SOL_SOCKET, SO_RCVBUF, &rcv, sizeof(rcv));

    fd_ = fd;
    socket_path_ = socket_path;
    socket_mode_ = mode;
    bound_path_owned_ = true;
    return true;
}

void Receiver::Close() {
    if (fd_ >= 0) {
        close(fd_);
        fd_ = -1;
    }
    if (bound_path_owned_ && !socket_path_.empty()) {
        unlink(socket_path_.c_str());
        bound_path_owned_ = false;
    }
}

void Receiver::ApplyGatewayFields(api::AnalyticsMetrics* metrics) const {
    if (!metrics) return;
    if (!gateway_id_.empty()) metrics->set_gatewayid(gateway_id_);
    if (!gateway_eui_bytes_.empty()) metrics->set_gatewayeui(gateway_eui_bytes_);
}

int Receiver::ParseDatagram(const char* buf, size_t n, api::AnalyticsMetrics* metrics) {
    if (!buf || !metrics || n < sizeof(struct ana_hdr)) {
        ++rejected_;
        return 0;
    }
    const auto* h = reinterpret_cast<const struct ana_hdr*>(buf);
    if (!ana_hdr_ok(h)) {
        ++rejected_;
        return 0;
    }
    switch (h->type) {
        case ANA_HELLO: {
            if (n != sizeof(struct ana_hello)) {
                ++rejected_;
                return 0;
            }
            const auto* hello = reinterpret_cast<const struct ana_hello*>(buf);
            gateway_eui_ = hello->gateway_eui;
            gateway_eui_hex_ = EuiToHex(gateway_eui_);
            gateway_eui_bytes_ = EuiToBytes(gateway_eui_);
            return 0;
        }
        case ANA_UPLINK: {
            if (n < sizeof(struct ana_uplink)) {
                ++rejected_;
                return 0;
            }
            const auto* up = reinterpret_cast<const struct ana_uplink*>(buf);
            if (up->size > ANA_MAX_PAYLOAD || n != ANA_UPLINK_LEN(up->size)) {
                ++rejected_;
                return 0;
            }
            const auto* payload = reinterpret_cast<const uint8_t*>(buf + sizeof(*up));
            FillUplink(up, payload, metrics->add_uplinks());
            ApplyGatewayFields(metrics);
            return 1;
        }
        case ANA_DOWNLINK: {
            if (n < sizeof(struct ana_downlink)) {
                ++rejected_;
                return 0;
            }
            const auto* dn = reinterpret_cast<const struct ana_downlink*>(buf);
            if (dn->size > ANA_MAX_PAYLOAD || n != ANA_DOWNLINK_LEN(dn->size)) {
                ++rejected_;
                return 0;
            }
            const auto* payload = reinterpret_cast<const uint8_t*>(buf + sizeof(*dn));
            FillDownlink(dn, payload, metrics->add_downlinks());
            ApplyGatewayFields(metrics);
            return 1;
        }
        case ANA_STAT: {
            if (n != sizeof(struct ana_stat)) {
                ++rejected_;
                return 0;
            }
            FillStat(reinterpret_cast<const struct ana_stat*>(buf), metrics->add_stats());
            ApplyGatewayFields(metrics);
            return 1;
        }
        default:
            ++rejected_;
            return 0;
    }
}

PollStats Receiver::PollEach(const std::function<void(api::AnalyticsMetrics&&)>& consume) {
    PollStats st;
    if(fd_<0)return st;
    char buf[kMaxDatagram];
    for(int i=0;i<max_poll_;++i){
      ssize_t n=recv(fd_,buf,sizeof(buf),MSG_TRUNC);
      if(n<0)break;
      if(static_cast<size_t>(n)>sizeof(buf)){++rejected_;++st.rejected;continue;}
      api::AnalyticsMetrics one;auto before=rejected_;
      int parsed=ParseDatagram(buf,static_cast<size_t>(n),&one);
      st.rejected+=static_cast<int>(rejected_-before);st.parsed+=parsed;
      if(parsed)consume(std::move(one));
    }
    return st;
}
PollStats Receiver::Poll(api::AnalyticsMetrics* metrics){
  if(!metrics)return {};
  return PollEach([&](api::AnalyticsMetrics&& one){metrics->MergeFrom(one);});
}

}  // namespace basicstation_analytics
