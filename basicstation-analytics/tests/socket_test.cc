#include "receiver.h"
#include "protocol.h"
#include "protobuf/analytics.pb.h"
#include "tests/test_harness.h"
#include <cstdlib>
#include <sys/stat.h>
#include <unistd.h>
using namespace basicstation_analytics;
using namespace bs_analytics_test;
int main(){
 char directory[]="/tmp/bs-rx-XXXXXX";
 Expect(mkdtemp(directory)!=nullptr,"private runtime directory");
 std::string path=std::string(directory)+"/analytics.sock",error;
 Receiver first,second;first.Configure(1024,1);
 Expect(first.Open(path,0660,&error),"first consumer binds");
 struct stat before{};stat(path.c_str(),&before);
 Expect(!second.Open(path,0660,&error),"second consumer denied");
 struct stat after{};stat(path.c_str(),&after);ExpectEq(before.st_ino,after.st_ino,"live socket not replaced");
 first.Close();Expect(second.Open(path,0660,&error),"restart reuses runtime lock");second.Close();
 std::string regular=std::string(directory)+"/regular";
 FILE* f=fopen(regular.c_str(),"w");fclose(f);
 Expect(!first.Open(regular,0660,&error),"regular file not replaced");
 ana_hello hello{};hello.hdr={ANA_MAGIC,ANA_VERSION,ANA_HELLO,0,ANA_ENDIAN};hello.gateway_eui=1;
 api::AnalyticsMetrics m;first.ParseDatagram(reinterpret_cast<const char*>(&hello),sizeof(hello),&m);
 hello.gateway_eui=2;first.ParseDatagram(reinterpret_cast<const char*>(&hello),sizeof(hello),&m);
 ExpectEq(first.GatewayEuiHex(),std::string("0000000000000001"),"identity cannot switch mid-stream");
 ExpectEq(first.Rejected(),uint64_t(1),"conflicting identity rejected");
 unlink(regular.c_str());unlink((regular+".lock").c_str());unlink((path+".lock").c_str());rmdir(directory);
 return Summary();
}
