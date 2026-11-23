#pragma once

#include <memory>
#include <string>
#include <optional>
#include <mutex>
#include <chrono>
#include <grpcpp/grpcpp.h>
#include "protobuf/api.pb.h"
#include "protobuf/api.grpc.pb.h"
#include "protobuf/analytics.pb.h"

namespace client_cc {

// Protocol version; must match analytics/client (v3: payload structure for older compilers).
constexpr int kClientVersion = 3;
inline constexpr const char* kDefaultAnalyticsEndpoint =
    "ingress.eu1.cluster.kudzu.gr:443";

struct AnalyticsClientConfig {
    std::string client_id;
    std::string client_key;
    std::string endpoint = kDefaultAnalyticsEndpoint;
    // Optional CA PEM file appended to the system trust store (additive).
    std::string ca_file;
    std::string ssl_target_name_override;
    int connect_timeout = 30; // seconds
    int request_timeout = 0; // seconds
    int max_reconnect_backoff = 60; // seconds
    std::optional<bool> auto_reconnect;
    std::optional<bool> server_side;
};

class Client {
public:
    explicit Client(const AnalyticsClientConfig& config);
    ~Client();

    using Deadline = std::chrono::system_clock::time_point;
    grpc::Status ConnectOnce(Deadline deadline);
    grpc::Status PushMetricsOnce(const api::AnalyticsMetrics& metrics, Deadline deadline);
    grpc::Status GatewayUpsertOnce(const api::ReqGatewayUpsert& req, Deadline deadline);
    // The only operation permitted concurrently with an RPC owner.
    void CancelPending();
    bool Connect();
    void Disconnect();
    const std::string& LastError() const;
    bool PushMetrics(const api::AnalyticsMetrics& metrics);
    bool GatewayUpsert(const api::ReqGatewayUpsert& req, api::RespGatewaySync* resp = nullptr);
    bool GatewayDelete(const api::ReqGatewayDelete& req, api::RespGatewaySync* resp = nullptr);

private:
    bool LoadTLSCredentials(std::shared_ptr<grpc::ChannelCredentials>& creds);
    struct ActiveCall {
      Client* owner;
      grpc::ClientContext context;
      ActiveCall(Client* owner, Deadline deadline);
      ~ActiveCall();
    };
    std::mutex call_mutex_;
    grpc::ClientContext* active_call_ = nullptr;
    bool cancelled_ = false;
    bool WithReconnect(const std::function<bool()>& fn);
    std::shared_ptr<grpc::Channel> channel_;
    std::unique_ptr<api::AnalyticsServer::Stub> stub_;
    AnalyticsClientConfig config_;
    std::string session_token_;
    std::string last_error_;
    bool connected_ = false;
};

} // namespace client_cc 