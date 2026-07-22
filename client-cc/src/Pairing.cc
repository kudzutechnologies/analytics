#include "AnalyticsPairing.h"
#include "TlsRoots.h"

#include <nlohmann/json.hpp>
#include <openssl/err.h>
#include <openssl/ssl.h>
#include <openssl/x509_vfy.h>

#include <arpa/inet.h>
#include <netdb.h>
#include <sys/socket.h>
#include <unistd.h>

#include <cstdlib>
#include <cstring>
#include <sstream>

namespace client_cc {
namespace {

constexpr const char* kPairingAPIPath = "/api/v1/pairing/edge";

bool ParseUrl(const std::string& url,
              std::string& host,
              int& port,
              std::string& path,
              std::string& authority) {
  if (url.size() < 8 || url.substr(0, 8) != "https://") return false;
  size_t start = 8;
  size_t slash = url.find('/', start);
  authority = slash == std::string::npos ? url.substr(start)
                                         : url.substr(start, slash - start);
  host = authority;
  path = slash == std::string::npos ? "/" : url.substr(slash);
  port = 443;
  size_t colon = host.rfind(':');
  if (colon != std::string::npos && colon > 0) {
    port = std::atoi(host.substr(colon + 1).c_str());
    if (port <= 0) port = 443;
    host = host.substr(0, colon);
  }
  return true;
}

std::string HttpsGet(const std::string& url,
                     const std::string& ca_file,
                     int* status_code,
                     std::string& error_msg) {
  std::string host, path;
  std::string authority;
  int port = 443;
  if (!ParseUrl(url, host, port, path, authority)) {
    error_msg = "invalid URL";
    return "";
  }

  struct addrinfo hints = {};
  hints.ai_family = AF_UNSPEC;
  hints.ai_socktype = SOCK_STREAM;
  struct addrinfo* res = nullptr;
  std::string port_str = std::to_string(port);
  int ret = getaddrinfo(host.c_str(), port_str.c_str(), &hints, &res);
  if (ret != 0 || !res) {
    error_msg = "failed to resolve host";
    return "";
  }

  int sock = -1;
  for (struct addrinfo* ai = res; ai != nullptr; ai = ai->ai_next) {
    sock = socket(ai->ai_family, ai->ai_socktype, ai->ai_protocol);
    if (sock < 0) continue;
    if (connect(sock, ai->ai_addr, static_cast<socklen_t>(ai->ai_addrlen)) == 0) break;
    close(sock);
    sock = -1;
  }
  freeaddrinfo(res);
  if (sock < 0) {
    error_msg = "connect failed";
    return "";
  }

  SSL_library_init();
  SSL_load_error_strings();
  SSL_CTX* ctx = SSL_CTX_new(TLS_client_method());
  if (!ctx) {
    close(sock);
    error_msg = "SSL_CTX_new failed";
    return "";
  }
  if (!ConfigureSSLContextTrust(ctx, ca_file, error_msg)) {
    SSL_CTX_free(ctx);
    close(sock);
    return "";
  }

  SSL* ssl = SSL_new(ctx);
  if (!ssl) {
    SSL_CTX_free(ctx);
    close(sock);
    error_msg = "SSL_new failed";
    return "";
  }
  SSL_set_fd(ssl, sock);
  SSL_set_tlsext_host_name(ssl, host.c_str());
#if OPENSSL_VERSION_NUMBER >= 0x10100000L
  SSL_set1_host(ssl, host.c_str());
#endif

  if (SSL_connect(ssl) != 1) {
    unsigned long err = ERR_get_error();
    char buf[256];
    ERR_error_string_n(err, buf, sizeof(buf));
    error_msg = std::string("SSL_connect failed: ") + buf;
    SSL_free(ssl);
    SSL_CTX_free(ctx);
    close(sock);
    return "";
  }

  long verify = SSL_get_verify_result(ssl);
  if (verify != X509_V_OK) {
    error_msg = std::string("certificate verification failed: ") +
                X509_verify_cert_error_string(verify);
    SSL_shutdown(ssl);
    SSL_free(ssl);
    SSL_CTX_free(ctx);
    close(sock);
    return "";
  }

  std::ostringstream req;
  req << "GET " << path << " HTTP/1.1\r\n"
      << "Host: " << authority << "\r\n"
      << "Connection: close\r\n\r\n";
  std::string req_str = req.str();
  int n = SSL_write(ssl, req_str.data(), static_cast<int>(req_str.size()));
  if (n <= 0 || static_cast<size_t>(n) != req_str.size()) {
    SSL_shutdown(ssl);
    SSL_free(ssl);
    SSL_CTX_free(ctx);
    close(sock);
    error_msg = "SSL_write failed";
    return "";
  }

  std::string body;
  char buf[4096];
  bool in_body = false;
  size_t content_length = 0;
  std::string header_block;
  int http_status = 0;

  while (true) {
    n = SSL_read(ssl, buf, sizeof(buf));
    if (n <= 0) break;
    if (!in_body) {
      header_block.append(buf, static_cast<size_t>(n));
      size_t sep = header_block.find("\r\n\r\n");
      if (sep != std::string::npos) {
        std::string headers = header_block.substr(0, sep);
        body = header_block.substr(sep + 4);
        if (headers.rfind("HTTP/", 0) == 0) {
          size_t sp1 = headers.find(' ');
          if (sp1 != std::string::npos) {
            http_status = std::atoi(headers.c_str() + sp1 + 1);
          }
        }
        std::string cl = "Content-Length:";
        size_t cl_pos = headers.find(cl);
        if (cl_pos != std::string::npos) {
          size_t num_start = headers.find_first_not_of(" \t", cl_pos + cl.size());
          if (num_start != std::string::npos) {
            content_length = static_cast<size_t>(std::atoi(headers.c_str() + num_start));
          }
        }
        in_body = true;
        while (content_length > 0 && body.size() < content_length) {
          n = SSL_read(ssl, buf, sizeof(buf));
          if (n <= 0) break;
          body.append(buf, static_cast<size_t>(n));
        }
        break;
      }
    } else {
      body.append(buf, static_cast<size_t>(n));
      if (content_length && body.size() >= content_length) break;
    }
  }

  SSL_shutdown(ssl);
  SSL_free(ssl);
  SSL_CTX_free(ctx);
  close(sock);

  if (status_code) *status_code = http_status;
  return body;
}

}  // namespace

std::string NormalizePairingPin(const std::string& pin) {
  std::string out;
  out.reserve(pin.size());
  for (unsigned char c : pin) {
    if (c >= '0' && c <= '9') out.push_back(static_cast<char>(c));
  }
  return out;
}

std::string PairingBaseURL(const PairingOptions& opts) {
  if (!opts.base_url.empty()) {
    std::string url = opts.base_url;
    while (!url.empty() && url.back() == '/') url.pop_back();
    return url;
  }
  if (!opts.endpoint.empty()) {
    std::string endpoint = opts.endpoint;
    while (!endpoint.empty() && endpoint.back() == '/') endpoint.pop_back();
    const size_t path_len = std::strlen(kPairingAPIPath);
    if (endpoint.size() >= path_len &&
        endpoint.compare(endpoint.size() - path_len, path_len, kPairingAPIPath) == 0) {
      return endpoint;
    }
    if (endpoint.find("://") == std::string::npos) {
      size_t colon = endpoint.find(':');
      if (colon != std::string::npos) endpoint = endpoint.substr(0, colon);
      endpoint = "https://" + endpoint;
    }
    return endpoint + kPairingAPIPath;
  }
  return kDefaultPairingBaseURL;
}

std::string DefaultPairingEndpoint() {
  std::string base_url = kDefaultPairingBaseURL;
  return base_url.substr(0, base_url.size() - std::strlen(kPairingAPIPath));
}

bool IsProtectedPairingConfigKey(const std::string& key) {
  return key == "client-id" || key == "client-key" || key == "gateway" ||
         key == "analytics-endpoint" || key == "pairing-endpoint" ||
         key == "analytics-ca-file" || key == "analytics-ssl-target-name" ||
         key == "config" || key == "pair-pin" || key == "write";
}

bool ParsePairingResponseJSON(const std::string& body,
                              PairingConfig& out,
                              std::string& error_msg) {
  nlohmann::json j;
  try {
    j = nlohmann::json::parse(body);
  } catch (...) {
    error_msg = "invalid JSON response";
    return false;
  }

  if (j.contains("error")) {
    if (j.contains("details") && j["details"].contains("description")) {
      error_msg = j["details"]["description"].get<std::string>();
    } else {
      error_msg = j["error"].get<std::string>();
    }
    return false;
  }

  if (!j.contains("data") || !j["data"].contains("config")) {
    error_msg = "no data in response";
    return false;
  }

  const auto& config = j["data"]["config"];
  out = PairingConfig{};
  if (config.contains("client-id")) out.client_id = config["client-id"].get<std::string>();
  if (config.contains("client-key")) out.client_key = config["client-key"].get<std::string>();
  if (config.contains("gateway")) out.gateway_id = config["gateway"].get<std::string>();

  if (config.contains("extras") && config["extras"].is_object()) {
    for (auto it = config["extras"].begin(); it != config["extras"].end(); ++it) {
      if (it.value().is_string()) {
        out.extras[it.key()] = it.value().get<std::string>();
      } else {
        out.extras[it.key()] = it.value().dump();
      }
    }
  }
  return true;
}

bool FetchPairingConfig(const PairingOptions& opts,
                        PairingConfig& out,
                        std::string& error_msg) {
  std::string pin = NormalizePairingPin(opts.pin);
  if (pin.empty()) {
    error_msg = "pairing PIN must contain digits";
    return false;
  }

  std::string url = PairingBaseURL(opts) + "/" + pin;
  int status = 0;
  std::string body = HttpsGet(url, opts.ca_file, &status, error_msg);
  if (body.empty() && !error_msg.empty()) return false;

  if (status < 200 || status >= 300) {
    error_msg = "pairing request failed with status " + std::to_string(status);
    return false;
  }

  return ParsePairingResponseJSON(body, out, error_msg);
}

}  // namespace client_cc
