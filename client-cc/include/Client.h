#pragma once

#include <memory>
#include <string>
#include <optional>
#include <grpcpp/grpcpp.h>
#include "protobuf/api.pb.h"
#include "protobuf/api.grpc.pb.h"
#include "protobuf/analytics.pb.h"

namespace client_cc {

// Protocol version; must match analytics/client (v3: payload structure for older compilers).
constexpr int kClientVersion = 3;

struct AnalyticsClientConfig {
    std::string client_id;
    std::string client_key;
    std::string endpoint = "analytics.v2.kudzu.gr:50051";
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

    bool Connect();
    void Disconnect();
    bool PushMetrics(const api::AnalyticsMetrics& metrics);
    bool GatewayUpsert(const api::ReqGatewayUpsert& req, api::RespGatewaySync* resp = nullptr);
    bool GatewayDelete(const api::ReqGatewayDelete& req, api::RespGatewaySync* resp = nullptr);

private:
    bool LoadTLSCredentials(std::shared_ptr<grpc::ChannelCredentials>& creds);
    bool Login(const std::string& challenge);
    bool WithReconnect(const std::function<bool()>& fn);
    std::shared_ptr<grpc::Channel> channel_;
    std::unique_ptr<api::AnalyticsServer::Stub> stub_;
    AnalyticsClientConfig config_;
    std::string session_token_;
    bool connected_ = false;
};

} // namespace client_cc 