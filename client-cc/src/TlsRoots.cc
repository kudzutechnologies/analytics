#include "TlsRoots.h"

#include <openssl/bio.h>
#include <openssl/err.h>
#include <openssl/pem.h>
#include <openssl/ssl.h>
#include <openssl/x509.h>
#include <openssl/x509_vfy.h>

#include <fstream>
#include <sstream>

namespace client_cc {
namespace {

std::string OpenSSLError() {
  unsigned long err = ERR_get_error();
  if (err == 0) return "unknown OpenSSL error";
  char buf[256];
  ERR_error_string_n(err, buf, sizeof(buf));
  return buf;
}

std::string ReadFile(const std::string& path, std::string& error_msg) {
  std::ifstream file(path, std::ios::binary);
  if (!file) {
    error_msg = "failed to open CA file: " + path;
    return "";
  }
  std::ostringstream ss;
  ss << file.rdbuf();
  std::string data = ss.str();
  if (data.empty()) {
    error_msg = "CA file is empty: " + path;
    return "";
  }
  return data;
}

bool StoreToPEM(X509_STORE* store, std::string& out_pem, std::string& error_msg) {
  STACK_OF(X509_OBJECT)* objs = X509_STORE_get0_objects(store);
  if (!objs) {
    error_msg = "failed to read X509_STORE objects";
    return false;
  }

  BIO* bio = BIO_new(BIO_s_mem());
  if (!bio) {
    error_msg = "BIO_new failed";
    return false;
  }

  int count = sk_X509_OBJECT_num(objs);
  int written = 0;
  for (int i = 0; i < count; ++i) {
    X509_OBJECT* obj = sk_X509_OBJECT_value(objs, i);
    if (!obj || X509_OBJECT_get_type(obj) != X509_LU_X509) continue;
    X509* cert = X509_OBJECT_get0_X509(obj);
    if (!cert) continue;
    if (PEM_write_bio_X509(bio, cert) != 1) {
      BIO_free(bio);
      error_msg = "failed to serialize certificate: " + OpenSSLError();
      return false;
    }
    ++written;
  }

  char* data = nullptr;
  long len = BIO_get_mem_data(bio, &data);
  if (len <= 0 || !data || written == 0) {
    BIO_free(bio);
    error_msg = "no root certificates loaded";
    return false;
  }
  out_pem.assign(data, static_cast<size_t>(len));
  BIO_free(bio);
  return true;
}

}  // namespace

bool LoadCombinedRootCertsPEM(const std::string& ca_file,
                              std::string& out_pem,
                              std::string& error_msg) {
  out_pem.clear();
  if (ca_file.empty()) {
    // Empty PEM tells gRPC to use platform/system defaults.
    return true;
  }

  std::string custom_pem = ReadFile(ca_file, error_msg);
  if (custom_pem.empty()) return false;

  X509_STORE* store = X509_STORE_new();
  if (!store) {
    error_msg = "X509_STORE_new failed";
    return false;
  }

  if (X509_STORE_set_default_paths(store) != 1) {
    X509_STORE_free(store);
    error_msg = "failed to load system root CAs: " + OpenSSLError();
    return false;
  }

  BIO* bio = BIO_new_mem_buf(custom_pem.data(), static_cast<int>(custom_pem.size()));
  if (!bio) {
    X509_STORE_free(store);
    error_msg = "BIO_new_mem_buf failed";
    return false;
  }

  int added = 0;
  while (true) {
    X509* cert = PEM_read_bio_X509(bio, nullptr, nullptr, nullptr);
    if (!cert) break;
    if (X509_STORE_add_cert(store, cert) == 1) {
      ++added;
    }
    X509_free(cert);
  }
  BIO_free(bio);

  // Clear the expected PEM EOF error.
  ERR_clear_error();

  if (added == 0) {
    X509_STORE_free(store);
    error_msg = "failed to add server CA's certificate";
    return false;
  }

  bool ok = StoreToPEM(store, out_pem, error_msg);
  X509_STORE_free(store);
  return ok;
}

bool ConfigureSSLContextTrust(void* ssl_ctx_void,
                              const std::string& ca_file,
                              std::string& error_msg) {
  auto* ctx = static_cast<SSL_CTX*>(ssl_ctx_void);
  if (!ctx) {
    error_msg = "null SSL_CTX";
    return false;
  }

  SSL_CTX_set_verify(ctx, SSL_VERIFY_PEER, nullptr);
  if (SSL_CTX_set_default_verify_paths(ctx) != 1) {
    error_msg = "failed to load system root CAs: " + OpenSSLError();
    return false;
  }

  if (!ca_file.empty()) {
    if (SSL_CTX_load_verify_locations(ctx, ca_file.c_str(), nullptr) != 1) {
      error_msg = "failed to load CA file: " + OpenSSLError();
      return false;
    }
  }
  return true;
}

}  // namespace client_cc
