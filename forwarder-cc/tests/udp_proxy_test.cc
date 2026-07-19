#include "tests/test_harness.h"
#include "udp_proxy.h"

#include <arpa/inet.h>
#include <chrono>
#include <cstring>
#include <netinet/in.h>
#include <sys/socket.h>
#include <thread>
#include <unistd.h>
#include <atomic>
#include <vector>

using namespace forwarder_cc;
using namespace forwarder_cc_test;

static int BindEphemeral(uint16_t* port_out) {
  int fd = socket(AF_INET, SOCK_DGRAM, 0);
  if (fd < 0) return -1;
  sockaddr_in addr{};
  addr.sin_family = AF_INET;
  addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
  addr.sin_port = 0;
  if (bind(fd, reinterpret_cast<sockaddr*>(&addr), sizeof(addr)) != 0) {
    close(fd);
    return -1;
  }
  socklen_t len = sizeof(addr);
  getsockname(fd, reinterpret_cast<sockaddr*>(&addr), &len);
  *port_out = ntohs(addr.sin_port);
  return fd;
}

int main() {
  // Shared-port proxy: listen 0 (ephemeral) up=down, connect to a local echo UDP port.
  uint16_t echo_port = 0;
  int echo_fd = BindEphemeral(&echo_port);
  Expect(echo_fd >= 0, "echo bind");

  std::atomic<bool> echo_run{true};
  std::thread echo_thread([&]() {
    std::vector<uint8_t> buf(2048);
    while (echo_run) {
      sockaddr_in from{};
      socklen_t fl = sizeof(from);
      ssize_t n = recvfrom(echo_fd, buf.data(), buf.size(), 0, reinterpret_cast<sockaddr*>(&from), &fl);
      if (n > 0) {
        sendto(echo_fd, buf.data(), static_cast<size_t>(n), 0, reinterpret_cast<sockaddr*>(&from), fl);
      }
    }
  });

  uint16_t listen_port = 0;
  // We'll let the proxy bind; pick a free port first then close.
  int tmp = BindEphemeral(&listen_port);
  Expect(tmp >= 0, "listen port probe");
  close(tmp);

  UdpProxyConfig cfg;
  cfg.listen_host = "127.0.0.1";
  cfg.listen_port_up = listen_port;
  cfg.listen_port_down = listen_port;  // shared
  cfg.connect_host = "127.0.0.1";
  cfg.connect_port_up = echo_port;
  cfg.connect_port_down = echo_port;
  cfg.buffer_size = 1500;
  cfg.max_streams = 4;
  cfg.reconnect_interval_sec = 1;

  std::atomic<int> up_local{0};
  std::atomic<int> up_remote{0};

  UdpProxy proxy(cfg);
  UdpProxyEvents ev;
  ev.up_local_data = [&](const uint8_t*, size_t, const SockAddr4&) { up_local++; };
  ev.up_remote_data = [&](const uint8_t*, size_t, const SockAddr4&) { up_remote++; };
  // Downlink Semtech on shared socket also fires dn callbacks from proxy UpThread.
  ev.dn_local_data = [&](const uint8_t*, size_t, const SockAddr4&) { up_local++; };
  proxy.SetEvents(std::move(ev));
  Expect(proxy.Start(), "proxy start");

  std::thread run_thread([&]() { proxy.Run(); });

  // Client sends Semtech PUSH_DATA-like header to proxy.
  int client = socket(AF_INET, SOCK_DGRAM, 0);
  Expect(client >= 0, "client sock");
  sockaddr_in dest{};
  dest.sin_family = AF_INET;
  dest.sin_port = htons(listen_port);
  inet_pton(AF_INET, "127.0.0.1", &dest.sin_addr);

  uint8_t pkt[12] = {2, 0, 1, 0x00, 1, 2, 3, 4, 5, 6, 7, 8};  // PUSH_DATA
  sendto(client, pkt, sizeof(pkt), 0, reinterpret_cast<sockaddr*>(&dest), sizeof(dest));

  // Wait for round-trip
  for (int i = 0; i < 50 && (up_local.load() == 0 || up_remote.load() == 0); ++i) {
    std::this_thread::sleep_for(std::chrono::milliseconds(20));
  }
  Expect(up_local.load() >= 1, "up local event");
  Expect(up_remote.load() >= 1, "up remote event (echo)");

  // Downlink kind on shared port
  uint8_t pull[12] = {2, 0, 2, 0x02, 1, 2, 3, 4, 5, 6, 7, 8};  // PULL_DATA
  sendto(client, pull, sizeof(pull), 0, reinterpret_cast<sockaddr*>(&dest), sizeof(dest));
  std::this_thread::sleep_for(std::chrono::milliseconds(100));
  Expect(up_local.load() >= 2, "shared-port downlink local event");

  close(client);
  proxy.Stop();
  run_thread.join();
  echo_run = false;
  // Wake echo thread
  uint8_t wake = 0;
  sockaddr_in self{};
  self.sin_family = AF_INET;
  self.sin_port = htons(echo_port);
  inet_pton(AF_INET, "127.0.0.1", &self.sin_addr);
  sendto(echo_fd, &wake, 1, 0, reinterpret_cast<sockaddr*>(&self), sizeof(self));
  echo_thread.join();
  close(echo_fd);

  return Summary();
}
