#include "receiver.h"
#include "protocol.h"
#include "protobuf/analytics.pb.h"
#include "tests/test_harness.h"
#include <nlohmann/json.hpp>
#include <cstring>
#include <fstream>
#include <iomanip>
#include <sstream>
#include <vector>
using namespace basicstation_analytics;
using namespace bs_analytics_test;
int main(){
 std::ifstream source(BS_RADIO_FIXTURE);nlohmann::json cases;source>>cases;
 for(const auto& c:cases){
  std::string hex=c.at("payload");std::vector<unsigned char> payload;
  for(size_t i=0;i<hex.size();i+=2)payload.push_back(static_cast<unsigned char>(std::stoul(hex.substr(i,2),nullptr,16)));
  ana_uplink wire{};wire.hdr={ANA_MAGIC,ANA_VERSION,ANA_UPLINK,0,ANA_ENDIAN};wire.sf=7;wire.bw=0;wire.freq=868100000;wire.crc_status=1;wire.size=payload.size();
  std::vector<char> datagram(sizeof(wire)+payload.size());std::memcpy(datagram.data(),&wire,sizeof(wire));std::memcpy(datagram.data()+sizeof(wire),payload.data(),payload.size());
  Receiver receiver;api::AnalyticsMetrics result;receiver.ParseDatagram(datagram.data(),datagram.size(),&result);
  ExpectEq(result.uplinks_size(),1,"golden datagram accepted");
  const auto& up=result.uplinks(0);ExpectEq(up.fhdr().size(),c.at("header_length").get<size_t>(),"golden header length");
  std::ostringstream hash;for(unsigned char byte:up.uniqueid())hash<<std::hex<<std::setw(2)<<std::setfill('0')<<static_cast<unsigned>(byte);
  ExpectEq(hash.str(),c.at("unique_id").get<std::string>(),"Go/C++ deployed dedup parity");
 }
 return Summary();
}
