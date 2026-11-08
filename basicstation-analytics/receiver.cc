/* SPDX-License-Identifier: MIT */
/* Copyright 2026 Kudzu Technologies */
#include "receiver.h"
#include "protocol.h"
#include "dedupe.h"
#include "lora_header.h"
#include "protobuf/analytics.pb.h"

#include <algorithm>
#include <cerrno>
#include <cstdio>
#include <cstring>
#include <fcntl.h>
#include <sys/socket.h>
#include <sys/file.h>
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
    const size_t fhdr_n = LoRaHeaderLength(payload,w->size);
    if (payload && fhdr_n > 0) {
        p->set_fhdr(payload, fhdr_n);
    }
    if (w->sf == 0) {
        p->set_modulation(api::FSK);
        p->set_dataratefsk(50000);
        p->set_codingrate(api::CR_OFF);
    } else {
        p->set_modulation(api::LORA);
        p->set_codingrate(api::CR_4_5);
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
    const size_t fhdr_n = LoRaHeaderLength(payload,w->size);
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
        p->set_dataratefsk(50000);
        p->set_codingrate(api::CR_OFF);
    } else {
        p->set_modulation(api::LORA);
        p->set_codingrate(api::CR_4_5);
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

bool Receiver::Open(const std::string& path,mode_t mode,std::string* err){
 if(fd_>=0){if(err)*err="receiver is already open";return false;}
 auto fail=[&](const std::string& reason){if(err)*err=reason;return false;};
 if(path.empty()||path.size()>=sizeof(sockaddr_un{}.sun_path))return fail("socket path is empty or too long");
 auto slash=path.find_last_of('/');std::string parent=slash==std::string::npos?".":(slash==0?"/":path.substr(0,slash));
 struct stat parent_stat{};
 if(stat(parent.c_str(),&parent_stat)!=0||!S_ISDIR(parent_stat.st_mode)||(parent_stat.st_mode&0007)!=0)return fail("socket directory must exist and deny access to other users (0750 or stricter)");
 if((mode&0007)!=0)return fail("socket mode must deny access to other users");
 int lockfd=open((path+".lock").c_str(),O_CREAT|O_RDWR|O_CLOEXEC|O_NOFOLLOW,0600);
 if(lockfd<0)return fail(std::string("lock open: ")+strerror(errno));
 struct stat lock_stat{};
 if(fstat(lockfd,&lock_stat)!=0||!S_ISREG(lock_stat.st_mode)||lock_stat.st_uid!=geteuid()||flock(lockfd,LOCK_EX|LOCK_NB)!=0){close(lockfd);return fail("socket already owned or invalid lock file");}
 int fd=socket(AF_UNIX,SOCK_DGRAM,0);
 if(fd<0){close(lockfd);return fail(std::string("socket: ")+strerror(errno));}
 auto cleanup=[&](const std::string& reason){close(fd);close(lockfd);return fail(reason);};
 struct stat st{};
 if(lstat(path.c_str(),&st)==0){if(!S_ISSOCK(st.st_mode)||st.st_uid!=geteuid())return cleanup("path exists and is not an owned socket");sockaddr_un probe_addr{};probe_addr.sun_family=AF_UNIX;std::memcpy(probe_addr.sun_path,path.c_str(),path.size()+1);
 int probe=socket(AF_UNIX,SOCK_DGRAM,0);if(probe<0)return cleanup("cannot inspect existing socket");
 int result=connect(probe,reinterpret_cast<sockaddr*>(&probe_addr),sizeof(probe_addr));int reason=errno;close(probe);
 if(result==0)return cleanup("existing socket has a live owner");if(reason!=ECONNREFUSED&&reason!=ENOENT)return cleanup("cannot verify stale socket");
 if(unlink(path.c_str())!=0)return cleanup("cannot remove stale socket");}
 sockaddr_un addr{};addr.sun_family=AF_UNIX;std::memcpy(addr.sun_path,path.c_str(),path.size()+1);
 if(bind(fd,reinterpret_cast<sockaddr*>(&addr),sizeof(addr))!=0)return cleanup(std::string("bind: ")+strerror(errno));
 if(chmod(path.c_str(),mode)!=0){unlink(path.c_str());return cleanup("cannot set socket permissions");}
 int flags=fcntl(fd,F_GETFL,0);
 if(flags<0||fcntl(fd,F_SETFL,flags|O_NONBLOCK)!=0||fcntl(fd,F_SETFD,FD_CLOEXEC)!=0||setsockopt(fd,SOL_SOCKET,SO_RCVBUF,&receive_buffer_,sizeof(receive_buffer_))!=0){unlink(path.c_str());return cleanup("cannot configure socket limits");}
 if(lstat(path.c_str(),&st)!=0){unlink(path.c_str());return cleanup("cannot inspect bound socket");}
 socklen_t option_size=sizeof(effective_buffer_);getsockopt(fd,SOL_SOCKET,SO_RCVBUF,&effective_buffer_,&option_size);
 fd_=fd;lock_fd_=lockfd;socket_dev_=st.st_dev;socket_ino_=st.st_ino;socket_path_=path;socket_mode_=mode;bound_path_owned_=true;
 return true;
}
void Receiver::Close(){
 if(fd_>=0){close(fd_);fd_=-1;}
 if(bound_path_owned_&&!socket_path_.empty()){
  struct stat st{};if(lstat(socket_path_.c_str(),&st)==0&&st.st_dev==socket_dev_&&st.st_ino==socket_ino_)unlink(socket_path_.c_str());
  bound_path_owned_=false;
 }
 if(lock_fd_>=0){close(lock_fd_);lock_fd_=-1;}
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
            if(hello->gateway_eui==0||(!gateway_eui_bytes_.empty()&&hello->gateway_eui!=gateway_eui_)){++rejected_;return 0;}
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
