#include "udp_proxy.h"
#include "logging.h"
#include "semtech_udp.h"

#include <arpa/inet.h>
#include <cerrno>
#include <chrono>
#include <cstring>
#include <netdb.h>
#include <netinet/in.h>
#include <openssl/evp.h>
#include <sys/socket.h>
#include <unistd.h>
#include <algorithm>
#include <vector>

namespace forwarder_cc {

namespace {

std::string AddrPortToString(uint32_t addr_net, uint16_t port_net) {
  char buf[64];
  struct in_addr a;
  a.s_addr = addr_net;
  const char* s = inet_ntop(AF_INET, &a, buf, sizeof(buf));
  if (!s) return "0.0.0.0:0";
  unsigned p = ntohs(port_net);
  std::snprintf(buf + std::strlen(buf), sizeof(buf) - std::strlen(buf), ":%u", p);
  return buf;
}

std::string AddrToString(uint32_t addr_net) {
  char buf[64];
  struct in_addr a;
  a.s_addr = addr_net;
  const char* s = inet_ntop(AF_INET, &a, buf, sizeof(buf));
  return s ? s : "0.0.0.0";
}

int ResolveUDP4(const std::string& host, int port, struct sockaddr_in* out) {
  std::memset(out, 0, sizeof(*out));
  out->sin_family = AF_INET;
  out->sin_port = htons(static_cast<uint16_t>(port));
  if (inet_pton(AF_INET, host.c_str(), &out->sin_addr) == 1) return 0;
  struct addrinfo hints = {};
  hints.ai_family = AF_INET;
  hints.ai_socktype = SOCK_DGRAM;
  struct addrinfo* res = nullptr;
  int ret = getaddrinfo(host.c_str(), nullptr, &hints, &res);
  if (ret != 0 || !res) return -1;
  struct sockaddr_in* in = reinterpret_cast<struct sockaddr_in*>(res->ai_addr);
  out->sin_addr = in->sin_addr;
  freeaddrinfo(res);
  return 0;
}

std::string Base64Encode(const uint8_t* data, size_t len) {
  if (!data || len == 0) return "";
  std::vector<unsigned char> out(((len + 2) / 3) * 4 + 1);
  int n = EVP_EncodeBlock(out.data(), data, static_cast<int>(len));
  if (n <= 0) return "";
  return std::string(reinterpret_cast<char*>(out.data()), static_cast<size_t>(n));
}

void RemoveKey(std::vector<std::string>& order, const std::string& key) {
  order.erase(std::remove(order.begin(), order.end(), key), order.end());
}

}  // namespace

bool operator==(const SockAddr4& a, const SockAddr4& b) {
  return a.addr == b.addr && a.port == b.port;
}

std::string SockAddrToString(const SockAddr4& a) {
  return AddrPortToString(a.addr, a.port);
}

std::string SockAddrIpString(const SockAddr4& a) {
  return AddrToString(a.addr);
}

struct UdpProxy::Stream {
  int remote_fd = -1;
  SockAddr4 local_ep;
  std::string name;
  std::atomic<bool> closed{false};
  std::thread reader_thread;
  UdpProxy* proxy = nullptr;
  bool is_up = true;

  void Close() {
    if (closed.exchange(true)) return;
    if (remote_fd >= 0) {
      ::shutdown(remote_fd, SHUT_RDWR);
      ::close(remote_fd);
      remote_fd = -1;
    }
  }

  void JoinReader() {
    if (reader_thread.joinable()) {
      // Avoid self-join if somehow called from reader thread.
      if (reader_thread.get_id() != std::this_thread::get_id()) {
        reader_thread.join();
      } else {
        reader_thread.detach();
      }
    }
  }

  void ReaderLoop() {
    std::vector<uint8_t> buf(proxy->config_.buffer_size);
    struct sockaddr_in to = {};
    to.sin_family = AF_INET;
    to.sin_addr.s_addr = local_ep.addr;
    to.sin_port = local_ep.port;
    socklen_t to_len = sizeof(to);

    while (!closed && remote_fd >= 0) {
      ssize_t n = recv(remote_fd, buf.data(), buf.size(), 0);
      if (n <= 0 || closed) break;
      int local_sock = is_up ? proxy->up_sock_ : proxy->dn_sock_;
      if (local_sock < 0 && proxy->shared_socket_) local_sock = proxy->up_sock_;
      if (local_sock >= 0) {
        sendto(local_sock, buf.data(), static_cast<size_t>(n), 0,
               reinterpret_cast<struct sockaddr*>(&to), to_len);
      }
      int sid = proxy->GetStreamId(local_ep);
      proxy->WriteDump(sid * 2 + 1, buf.data(), static_cast<size_t>(n));
      if (n >= 4) {
        if (is_up || proxy->shared_socket_) {
          if (SemtechUDPIsUplink(buf.data())) {
            if (proxy->events_.up_remote_data)
              proxy->events_.up_remote_data(buf.data(), static_cast<size_t>(n), local_ep);
          } else if (SemtechUDPIsDownlink(buf.data())) {
            if (proxy->events_.dn_remote_data)
              proxy->events_.dn_remote_data(buf.data(), static_cast<size_t>(n), local_ep);
          }
        } else {
          if (proxy->events_.dn_remote_data)
            proxy->events_.dn_remote_data(buf.data(), static_cast<size_t>(n), local_ep);
        }
      }
    }
    Close();
  }
};

void UdpProxy::EvictOldest(StreamMap& map, std::mutex& /*mu*/, int max_size) {
  // Caller holds the map mutex. Use insertion-order vectors stored alongside.
  // This method is specialized below via GetUpStream/GetDnStream helpers.
  (void)map;
  (void)max_size;
}

int UdpProxy::GetStreamId(const SockAddr4& local_ep) {
  std::string key = SockAddrToString(local_ep);
  std::lock_guard<std::mutex> lock(up_mutex_);
  auto it = stream_ids_.find(key);
  if (it != stream_ids_.end()) return it->second;
  int slot = last_stream_id_++;
  stream_ids_[key] = slot;
  return slot;
}

void UdpProxy::WriteDump(int stream_index, const uint8_t* data, size_t len) {
  if (!dump_file_ || !data || len == 0) return;
  std::string b64 = Base64Encode(data, len);
  std::lock_guard<std::mutex> lock(dump_mutex_);
  if (!dump_file_) return;
  std::fprintf(dump_file_, "%d:%s\n", stream_index, b64.c_str());
  std::fflush(dump_file_);
}

UdpProxy::Stream* UdpProxy::GetUpStream(const SockAddr4& local_ep) {
  std::string key = SockAddrToString(local_ep);
  std::lock_guard<std::mutex> lock(up_mutex_);
  auto it = up_streams_.find(key);
  if (it != up_streams_.end()) return it->second.get();

  while (config_.max_streams > 0 &&
         static_cast<int>(up_streams_.size()) >= config_.max_streams &&
         !up_order_.empty()) {
    const std::string& oldest = up_order_.front();
    auto eit = up_streams_.find(oldest);
    if (eit != up_streams_.end()) {
      eit->second->Close();
      eit->second->JoinReader();
      up_streams_.erase(eit);
    }
    up_order_.erase(up_order_.begin());
  }

  struct sockaddr_in remote_addr = {};
  if (ResolveUDP4(config_.connect_host, config_.connect_port_up, &remote_addr) != 0) return nullptr;

  int fd = socket(AF_INET, SOCK_DGRAM, 0);
  if (fd < 0) return nullptr;

  if (!config_.connect_interface.empty() && config_.connect_interface != "0.0.0.0") {
    struct sockaddr_in bind_addr = {};
    bind_addr.sin_family = AF_INET;
    bind_addr.sin_port = 0;
    if (inet_pton(AF_INET, config_.connect_interface.c_str(), &bind_addr.sin_addr) == 1) {
      bind(fd, reinterpret_cast<struct sockaddr*>(&bind_addr), sizeof(bind_addr));
    }
  }

  if (connect(fd, reinterpret_cast<struct sockaddr*>(&remote_addr), sizeof(remote_addr)) != 0) {
    close(fd);
    return nullptr;
  }

  auto s = std::make_unique<Stream>();
  s->remote_fd = fd;
  s->local_ep = local_ep;
  s->name = "up:" + key;
  s->proxy = this;
  s->is_up = true;
  s->reader_thread = std::thread(&Stream::ReaderLoop, s.get());
  UdpProxy::Stream* raw = s.get();
  up_streams_[key] = std::move(s);
  up_order_.push_back(key);
  return raw;
}

UdpProxy::Stream* UdpProxy::GetDnStream(const SockAddr4& local_ep) {
  std::string key = SockAddrToString(local_ep);
  std::lock_guard<std::mutex> lock(dn_mutex_);
  auto it = dn_streams_.find(key);
  if (it != dn_streams_.end()) return it->second.get();

  while (config_.max_streams > 0 &&
         static_cast<int>(dn_streams_.size()) >= config_.max_streams &&
         !dn_order_.empty()) {
    const std::string& oldest = dn_order_.front();
    auto eit = dn_streams_.find(oldest);
    if (eit != dn_streams_.end()) {
      eit->second->Close();
      eit->second->JoinReader();
      dn_streams_.erase(eit);
    }
    dn_order_.erase(dn_order_.begin());
  }

  struct sockaddr_in remote_addr = {};
  if (ResolveUDP4(config_.connect_host, config_.connect_port_down, &remote_addr) != 0) return nullptr;

  int fd = socket(AF_INET, SOCK_DGRAM, 0);
  if (fd < 0) return nullptr;

  if (!config_.connect_interface.empty() && config_.connect_interface != "0.0.0.0") {
    struct sockaddr_in bind_addr = {};
    bind_addr.sin_family = AF_INET;
    bind_addr.sin_port = 0;
    if (inet_pton(AF_INET, config_.connect_interface.c_str(), &bind_addr.sin_addr) == 1) {
      bind(fd, reinterpret_cast<struct sockaddr*>(&bind_addr), sizeof(bind_addr));
    }
  }

  if (connect(fd, reinterpret_cast<struct sockaddr*>(&remote_addr), sizeof(remote_addr)) != 0) {
    close(fd);
    return nullptr;
  }

  auto s = std::make_unique<Stream>();
  s->remote_fd = fd;
  s->local_ep = local_ep;
  s->name = "dn:" + key;
  s->proxy = this;
  s->is_up = false;
  s->reader_thread = std::thread(&Stream::ReaderLoop, s.get());
  UdpProxy::Stream* raw = s.get();
  dn_streams_[key] = std::move(s);
  dn_order_.push_back(key);
  return raw;
}

UdpProxy::UdpProxy(const UdpProxyConfig& config) : config_(config) {
  if (!config_.debug_dump.empty()) {
    dump_file_ = std::fopen(config_.debug_dump.c_str(), "a");
    if (!dump_file_) {
      LOG_WARN() << "Could not open debug-dump file: " << config_.debug_dump;
    }
  }
}

UdpProxy::~UdpProxy() {
  Stop();
  if (dump_file_) {
    std::fclose(dump_file_);
    dump_file_ = nullptr;
  }
}

void UdpProxy::SetEvents(UdpProxyEvents events) {
  events_ = std::move(events);
}

bool UdpProxy::BindLocal() {
  struct sockaddr_in up_bind = {};
  up_bind.sin_family = AF_INET;
  up_bind.sin_port = htons(static_cast<uint16_t>(config_.listen_port_up));
  if (config_.listen_host.empty() || config_.listen_host == "0.0.0.0") {
    up_bind.sin_addr.s_addr = INADDR_ANY;
  } else if (inet_pton(AF_INET, config_.listen_host.c_str(), &up_bind.sin_addr) != 1) {
    return false;
  }

  up_sock_ = socket(AF_INET, SOCK_DGRAM, 0);
  if (up_sock_ < 0) return false;
  int yes = 1;
  setsockopt(up_sock_, SOL_SOCKET, SO_REUSEADDR, &yes, sizeof(yes));
  if (bind(up_sock_, reinterpret_cast<struct sockaddr*>(&up_bind), sizeof(up_bind)) != 0) {
    close(up_sock_);
    up_sock_ = -1;
    return false;
  }

  shared_socket_ = (config_.listen_port_down == config_.listen_port_up);
  if (!shared_socket_) {
    struct sockaddr_in dn_bind = up_bind;
    dn_bind.sin_port = htons(static_cast<uint16_t>(config_.listen_port_down));
    dn_sock_ = socket(AF_INET, SOCK_DGRAM, 0);
    if (dn_sock_ < 0) {
      close(up_sock_);
      up_sock_ = -1;
      return false;
    }
    setsockopt(dn_sock_, SOL_SOCKET, SO_REUSEADDR, &yes, sizeof(yes));
    if (bind(dn_sock_, reinterpret_cast<struct sockaddr*>(&dn_bind), sizeof(dn_bind)) != 0) {
      close(dn_sock_);
      close(up_sock_);
      dn_sock_ = -1;
      up_sock_ = -1;
      return false;
    }
  } else {
    dn_sock_ = up_sock_;
  }
  return true;
}

bool UdpProxy::Start() {
  if (!BindLocal()) return false;
  running_ = true;
  LOG_INFO() << "[up] Listening on " << config_.listen_host << ":" << config_.listen_port_up;
  if (shared_socket_) {
    LOG_INFO() << "[dn] Also listening on " << config_.listen_host << ":" << config_.listen_port_up;
  } else {
    LOG_INFO() << "[dn] Listening on " << config_.listen_host << ":" << config_.listen_port_down;
  }
  return true;
}

void UdpProxy::CloseSockets() {
  int up = up_sock_;
  int dn = dn_sock_;
  up_sock_ = -1;
  dn_sock_ = -1;
  if (up >= 0) {
    ::shutdown(up, SHUT_RDWR);
    ::close(up);
  }
  if (dn >= 0 && dn != up) {
    ::shutdown(dn, SHUT_RDWR);
    ::close(dn);
  }
}

void UdpProxy::JoinWorkerThreads() {
  if (up_thread_.joinable() && up_thread_.get_id() != std::this_thread::get_id()) {
    up_thread_.join();
  }
  if (dn_thread_.joinable() && dn_thread_.get_id() != std::this_thread::get_id()) {
    dn_thread_.join();
  }
}

void UdpProxy::ScheduleRestart() {
  if (!running_) return;
  bool expected = false;
  if (!restarting_.compare_exchange_strong(expected, true)) return;

  LOG_INFO() << "Restarting sockets in " << config_.reconnect_interval_sec << " seconds";
  CloseSockets();

  if (restart_thread_.joinable() && restart_thread_.get_id() != std::this_thread::get_id()) {
    restart_thread_.join();
  }
  restart_thread_ = std::thread([this]() {
    JoinWorkerThreads();
    std::this_thread::sleep_for(std::chrono::seconds(
        config_.reconnect_interval_sec > 0 ? config_.reconnect_interval_sec : 1));
    if (!running_) {
      restarting_ = false;
      return;
    }
    if (!BindLocal()) {
      LOG_WARN() << "Could not rebind local sockets; retrying";
      restarting_ = false;
      ScheduleRestart();
      return;
    }
    up_thread_ = std::thread(&UdpProxy::UpThread, this);
    if (!shared_socket_) {
      dn_thread_ = std::thread(&UdpProxy::DnThread, this);
    }
    restarting_ = false;
  });
}

void UdpProxy::Run() {
  up_thread_ = std::thread(&UdpProxy::UpThread, this);
  if (!shared_socket_) {
    dn_thread_ = std::thread(&UdpProxy::DnThread, this);
  }
  while (running_) {
    std::this_thread::sleep_for(std::chrono::milliseconds(200));
    // Keep Run() alive across socket restarts until Stop().
    if (!restarting_ && up_sock_ < 0 && running_) {
      ScheduleRestart();
    }
  }
  JoinWorkerThreads();
  if (restart_thread_.joinable()) restart_thread_.join();
}

void UdpProxy::Stop() {
  running_ = false;
  CloseSockets();
  JoinWorkerThreads();
  if (restart_thread_.joinable() && restart_thread_.get_id() != std::this_thread::get_id()) {
    restart_thread_.join();
  }
  {
    std::lock_guard<std::mutex> lock(up_mutex_);
    for (auto& p : up_streams_) {
      p.second->Close();
      p.second->JoinReader();
    }
    up_streams_.clear();
    up_order_.clear();
  }
  {
    std::lock_guard<std::mutex> lock(dn_mutex_);
    for (auto& p : dn_streams_) {
      p.second->Close();
      p.second->JoinReader();
    }
    dn_streams_.clear();
    dn_order_.clear();
  }
}

void UdpProxy::UpThread() {
  std::vector<uint8_t> buf(config_.buffer_size);
  struct sockaddr_in from = {};
  socklen_t from_len = sizeof(from);

  while (running_ && up_sock_ >= 0) {
    from_len = sizeof(from);
    ssize_t n = recvfrom(up_sock_, buf.data(), buf.size(), 0,
                         reinterpret_cast<struct sockaddr*>(&from), &from_len);
    if (!running_) break;
    if (n < 0) {
      if (errno == EINTR) continue;
      LOG_WARN() << "[up] recvfrom failed: " << std::strerror(errno);
      ScheduleRestart();
      break;
    }
    if (n == 0) continue;

    SockAddr4 local_ep;
    local_ep.addr = from.sin_addr.s_addr;
    local_ep.port = from.sin_port;

    const uint8_t* data = buf.data();
    size_t len = static_cast<size_t>(n);

    // On shared socket, route by Semtech kind for stream selection.
    bool as_downlink = shared_socket_ && len >= 4 && SemtechUDPIsDownlink(data);
    Stream* stream = as_downlink ? GetDnStream(local_ep) : GetUpStream(local_ep);
    if (stream && stream->remote_fd >= 0) {
      ssize_t sent = send(stream->remote_fd, data, len, 0);
      if (sent != static_cast<ssize_t>(len)) {
        stream->Close();
        std::string key = SockAddrToString(local_ep);
        if (as_downlink) {
          std::lock_guard<std::mutex> lock(dn_mutex_);
          auto it = dn_streams_.find(key);
          if (it != dn_streams_.end()) {
            it->second->JoinReader();
            dn_streams_.erase(it);
          }
          RemoveKey(dn_order_, key);
        } else {
          std::lock_guard<std::mutex> lock(up_mutex_);
          auto it = up_streams_.find(key);
          if (it != up_streams_.end()) {
            it->second->JoinReader();
            up_streams_.erase(it);
          }
          RemoveKey(up_order_, key);
        }
      }
    }
    int sid = GetStreamId(local_ep);
    WriteDump(sid * 2 + 0, data, len);
    if (as_downlink) {
      if (events_.dn_local_data) events_.dn_local_data(data, len, local_ep);
    } else {
      if (events_.up_local_data) events_.up_local_data(data, len, local_ep);
    }
  }
}

void UdpProxy::DnThread() {
  if (shared_socket_ || dn_sock_ < 0) return;
  std::vector<uint8_t> buf(config_.buffer_size);
  struct sockaddr_in from = {};
  socklen_t from_len = sizeof(from);

  while (running_ && dn_sock_ >= 0) {
    from_len = sizeof(from);
    ssize_t n = recvfrom(dn_sock_, buf.data(), buf.size(), 0,
                         reinterpret_cast<struct sockaddr*>(&from), &from_len);
    if (!running_) break;
    if (n < 0) {
      if (errno == EINTR) continue;
      LOG_WARN() << "[dn] recvfrom failed: " << std::strerror(errno);
      ScheduleRestart();
      break;
    }
    if (n == 0) continue;

    SockAddr4 local_ep;
    local_ep.addr = from.sin_addr.s_addr;
    local_ep.port = from.sin_port;

    const uint8_t* data = buf.data();
    size_t len = static_cast<size_t>(n);
    Stream* stream = GetDnStream(local_ep);
    if (stream && stream->remote_fd >= 0) {
      ssize_t sent = send(stream->remote_fd, data, len, 0);
      if (sent != static_cast<ssize_t>(len)) {
        stream->Close();
        std::string key = SockAddrToString(local_ep);
        std::lock_guard<std::mutex> lock(dn_mutex_);
        auto it = dn_streams_.find(key);
        if (it != dn_streams_.end()) {
          it->second->JoinReader();
          dn_streams_.erase(it);
        }
        RemoveKey(dn_order_, key);
      }
    }
    int sid = GetStreamId(local_ep);
    WriteDump(sid * 2 + 0, data, len);
    if (events_.dn_local_data) events_.dn_local_data(data, len, local_ep);
  }
}

}  // namespace forwarder_cc
