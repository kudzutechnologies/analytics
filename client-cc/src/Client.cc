#include "Client.h"
#include "TlsRoots.h"
#include <chrono>
#include <thread>
#include <openssl/sha.h>

namespace client_cc {

namespace {
std::string HexToBytes(const std::string& hex) {
    std::string bytes;
    bytes.reserve(hex.length() / 2);
    for (size_t i = 0; i < hex.length(); i += 2) {
        std::string byteString = hex.substr(i, 2);
        char byte = (char) strtol(byteString.c_str(), nullptr, 16);
        bytes.push_back(byte);
    }
    return bytes;
}

std::string Sha256Hex(const std::string& input) {
    unsigned char hash[SHA256_DIGEST_LENGTH];
    SHA256((const unsigned char*)input.data(), input.size(), hash);
    return std::string(reinterpret_cast<char*>(hash), SHA256_DIGEST_LENGTH);
}
} // namespace

Client::Client(const AnalyticsClientConfig& config)
    : config_(config) {}

Client::~Client() {
    Disconnect();
}

bool Client::LoadTLSCredentials(std::shared_ptr<grpc::ChannelCredentials>& creds) {
    grpc::SslCredentialsOptions ssl_opts;
    std::string error_msg;
    // Empty ca_file => empty pem_root_certs => gRPC platform/system defaults.
    // Non-empty ca_file => system roots + custom CA PEM.
    if (!LoadCombinedRootCertsPEM(config_.ca_file, ssl_opts.pem_root_certs, error_msg)) {
        return false;
    }
    creds = grpc::SslCredentials(ssl_opts);
    return true;
}

bool Client::Connect() {
    Disconnect();
    std::shared_ptr<grpc::ChannelCredentials> creds;
    if (!LoadTLSCredentials(creds)) return false;
    grpc::ChannelArguments args;
    if (!config_.ssl_target_name_override.empty()) {
        args.SetSslTargetNameOverride(config_.ssl_target_name_override);
    }
    auto deadline = std::chrono::system_clock::now() + std::chrono::seconds(config_.connect_timeout);
    channel_ = grpc::CreateCustomChannel(config_.endpoint, creds, args);
    stub_ = api::AnalyticsServer::NewStub(channel_);
    grpc::ClientContext ctx;
    ctx.set_deadline(deadline);
    api::ReqHello hello_req;
    hello_req.set_version(kClientVersion);
    api::RespHello hello_resp;
    auto status = stub_->Hello(&ctx, hello_req, &hello_resp);
    if (!status.ok()) return false;
    if (!Login(hello_resp.challenge())) return false;
    connected_ = true;
    return true;
}

bool Client::Login(const std::string& challenge) {
    grpc::ClientContext ctx;
    api::ReqLogin login_req;
    login_req.set_clientid(HexToBytes(config_.client_id));
    std::string key_bytes = HexToBytes(config_.client_key);
    std::string to_hash = challenge + "|" + key_bytes;
    login_req.set_hash(Sha256Hex(to_hash));
    login_req.set_serverside(config_.server_side.value_or(false));
    api::RespLogin login_resp;
    auto status = stub_->Login(&ctx, login_req, &login_resp);
    if (!status.ok()) return false;
    session_token_ = login_resp.accesstoken();
    return true;
}

void Client::Disconnect() {
    stub_.reset();
    channel_.reset();
    connected_ = false;
}

bool Client::WithReconnect(const std::function<bool()>& fn) {
    int backoff = 1;
    int max_backoff = config_.max_reconnect_backoff;
    bool auto_reconnect = config_.auto_reconnect.value_or(true);
    do {
        if (!connected_) {
            if (!Connect()) {
                std::this_thread::sleep_for(std::chrono::seconds(backoff));
                backoff = std::min(backoff * 2, max_backoff);
                continue;
            }
        }
        if (fn()) return true;
        Disconnect();
        std::this_thread::sleep_for(std::chrono::seconds(backoff));
        backoff = std::min(backoff * 2, max_backoff);
    } while (auto_reconnect);
    return false;
}

bool Client::PushMetrics(const api::AnalyticsMetrics& metrics) {
    if (!connected_) return false;
    return WithReconnect([&]() {
        grpc::ClientContext ctx;
        ctx.AddMetadata("token", session_token_);
        if (config_.request_timeout > 0) {
            auto deadline = std::chrono::system_clock::now() + std::chrono::seconds(config_.request_timeout);
            ctx.set_deadline(deadline);
        }
        api::RespPush resp;
        auto status = stub_->PushMetrics(&ctx, metrics, &resp);
        return status.ok();
    });
}

bool Client::GatewayUpsert(const api::ReqGatewayUpsert& req, api::RespGatewaySync* resp) {
    if (!connected_) return false;
    return WithReconnect([&]() {
        grpc::ClientContext ctx;
        ctx.AddMetadata("token", session_token_);
        if (config_.request_timeout > 0) {
            auto deadline = std::chrono::system_clock::now() + std::chrono::seconds(config_.request_timeout);
            ctx.set_deadline(deadline);
        }
        api::RespGatewaySync local;
        api::RespGatewaySync* out = resp != nullptr ? resp : &local;
        auto status = stub_->GatewayUpsert(&ctx, req, out);
        return status.ok();
    });
}

bool Client::GatewayDelete(const api::ReqGatewayDelete& req, api::RespGatewaySync* resp) {
    if (!connected_) return false;
    return WithReconnect([&]() {
        grpc::ClientContext ctx;
        ctx.AddMetadata("token", session_token_);
        if (config_.request_timeout > 0) {
            auto deadline = std::chrono::system_clock::now() + std::chrono::seconds(config_.request_timeout);
            ctx.set_deadline(deadline);
        }
        api::RespGatewaySync local;
        api::RespGatewaySync* out = resp != nullptr ? resp : &local;
        auto status = stub_->GatewayDelete(&ctx, req, out);
        return status.ok();
    });
}

} // namespace client_cc 