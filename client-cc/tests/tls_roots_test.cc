#include "TlsRoots.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <string>
#include <vector>
#include <unistd.h>

namespace {

int failures = 0;

void Expect(bool cond, const char* msg) {
  if (!cond) {
    std::cerr << "FAIL: " << msg << "\n";
    ++failures;
  }
}

std::string WriteTemp(const std::string& contents, const char* suffix) {
  std::string path = std::string("/tmp/client_cc_XXXXXX") + suffix;
  std::vector<char> buf(path.begin(), path.end());
  buf.push_back('\0');
  int fd = mkstemps(buf.data(), static_cast<int>(std::strlen(suffix)));
  if (fd < 0) return "";
  ssize_t n = write(fd, contents.data(), contents.size());
  close(fd);
  if (n < 0 || static_cast<size_t>(n) != contents.size()) return "";
  return std::string(buf.data());
}

}  // namespace

int main() {
  using client_cc::LoadCombinedRootCertsPEM;

  std::string pem;
  std::string err;

  Expect(LoadCombinedRootCertsPEM("", pem, err), "empty ca_file should succeed");
  Expect(pem.empty(), "empty ca_file should leave PEM empty for gRPC defaults");

  Expect(!LoadCombinedRootCertsPEM("/no/such/ca.pem", pem, err),
         "missing CA file should fail");
  Expect(!err.empty(), "missing CA file should set error");

  std::string bad = WriteTemp("not-a-certificate\n", ".pem");
  Expect(!bad.empty(), "temp file created");
  Expect(!LoadCombinedRootCertsPEM(bad, pem, err), "malformed CA should fail");
  std::remove(bad.c_str());

  char cert_path[] = "/tmp/client_cc_certXXXXXX.pem";
  char key_path[] = "/tmp/client_cc_keyXXXXXX.pem";
  int cfd = mkstemps(cert_path, 4);
  int kfd = mkstemps(key_path, 4);
  if (cfd >= 0 && kfd >= 0) {
    close(cfd);
    close(kfd);
    std::string cmd = std::string("openssl req -x509 -newkey rsa:2048 -nodes ") +
                      "-keyout " + key_path + " -out " + cert_path +
                      " -subj /CN=test-ca -days 1 >/dev/null 2>&1";
    if (std::system(cmd.c_str()) == 0) {
      Expect(LoadCombinedRootCertsPEM(cert_path, pem, err), "valid CA should succeed");
      Expect(pem.find("BEGIN CERTIFICATE") != std::string::npos,
             "combined PEM should contain certificates");
    }
    std::remove(cert_path);
    std::remove(key_path);
  }

  if (failures) {
    std::cerr << failures << " failure(s)\n";
    return 1;
  }
  std::cout << "ok\n";
  return 0;
}
