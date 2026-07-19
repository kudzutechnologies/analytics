#pragma once

#include <atomic>
#include <cstdio>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <unordered_map>
#include <vector>

namespace forwarder_cc {

struct SockAddr4 {
  uint32_t addr = 0;  // network byte order
  uint16_t port = 0;  // network byte order
};
bool operator==(const SockAddr4& a, const SockAddr4& b);
std::string SockAddrToString(const SockAddr4& a);
std::string SockAddrIpString(const SockAddr4& a);

struct UdpProxyConfig {
  std::string listen_host;
  int listen_port_up = 1800;
  int listen_port_down = 1801;
  std::string connect_host;
  int connect_port_up = 1700;
  int connect_port_down = 1700;
  std::string connect_interface;
  int buffer_size = 1500;
  int max_streams = 2;
  int reconnect_interval_sec = 1;  // connect-retry-interval
  std::string debug_dump;          // empty = disabled
};

struct UdpProxyEvents {
  std::function<void(const uint8_t* data, size_t len, const SockAddr4& local_ep)> up_local_data;
  std::function<void(const uint8_t* data, size_t len, const SockAddr4& local_ep)> up_remote_data;
  std::function<void(const uint8_t* data, size_t len, const SockAddr4& local_ep)> dn_local_data;
  std::function<void(const uint8_t* data, size_t len, const SockAddr4& local_ep)> dn_remote_data;
};

class UdpProxy {
 public:
  explicit UdpProxy(const UdpProxyConfig& config);
  ~UdpProxy();

  void SetEvents(UdpProxyEvents events);
  bool Start();
  void Stop();

  // Blocks until Stop. Starts internal receive threads.
  void Run();

 private:
  class Stream;
  using StreamMap = std::unordered_map<std::string, std::unique_ptr<Stream>>;

  void UpThread();
  void DnThread();
  Stream* GetUpStream(const SockAddr4& local_ep);
  Stream* GetDnStream(const SockAddr4& local_ep);
  int GetStreamId(const SockAddr4& local_ep);
  void EvictOldest(StreamMap& map, std::mutex& mu, int max_size);
  void WriteDump(int stream_index, const uint8_t* data, size_t len);
  bool BindLocal();
  void CloseSockets();
  void ScheduleRestart();
  void JoinWorkerThreads();

  UdpProxyConfig config_;
  UdpProxyEvents events_;
  int up_sock_ = -1;
  int dn_sock_ = -1;
  bool shared_socket_ = false;
  std::atomic<bool> running_{false};
  std::atomic<bool> restarting_{false};
  std::mutex up_mutex_;
  StreamMap up_streams_;
  std::vector<std::string> up_order_;
  std::mutex dn_mutex_;
  StreamMap dn_streams_;
  std::vector<std::string> dn_order_;
  std::unordered_map<std::string, int> stream_ids_;
  int last_stream_id_ = 0;
  std::thread up_thread_;
  std::thread dn_thread_;
  std::thread restart_thread_;
  std::mutex dump_mutex_;
  FILE* dump_file_ = nullptr;
};

}  // namespace forwarder_cc
