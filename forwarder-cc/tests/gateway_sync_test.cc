#include "gateway_sync.h"
#include "tests/test_harness.h"

#include <iomanip>
#include <sstream>

using namespace forwarder_cc;
using namespace forwarder_cc_test;

static std::string HexEncode(const std::vector<uint8_t>& b) {
  std::ostringstream ss;
  for (uint8_t v : b) {
    ss << std::hex << std::setw(2) << std::setfill('0') << static_cast<int>(v);
  }
  return ss.str();
}

int main() {
  {
    ParsedGatewaySync parsed;
    std::string err;
    Expect(!ParseGatewaySyncConfig(Config{}, parsed, err), "none: returns false");
    Expect(err.empty(), "none: no error");
    Expect(!parsed.has_any, "none: has_any false");
  }

  {
    Config c;
    c.gateway_name = "gw";
    ParsedGatewaySync parsed;
    std::string err;
    Expect(!ParseGatewaySyncConfig(c, parsed, err), "name-only fails");
    Expect(!err.empty(), "name-only error set");
  }

  {
    api::GatewayLocation loc;
    std::string err;
    Expect(ParseGatewayLocation("37.98,23.72", loc, err), "loc 2-part");
    ExpectNear(loc.latitude(), 37.98, 1e-9, "lat");
    ExpectNear(loc.longitude(), 23.72, 1e-9, "lon");
    Expect(!loc.has_altitude(), "no alt");

    Expect(ParseGatewayLocation("37.98,23.72,12.5", loc, err), "loc 3-part");
    Expect(loc.has_altitude(), "has alt");
    ExpectNear(loc.altitude(), 12.5, 1e-9, "alt");

    Expect(!ParseGatewayLocation("91,0", loc, err), "lat out of range");
    Expect(!ParseGatewayLocation("0,181", loc, err), "lon out of range");
    Expect(!ParseGatewayLocation("1", loc, err), "too few parts");
  }

  {
    std::vector<uint8_t> eui;
    std::string err;
    Expect(ParseGatewayEUI("01:02:03:04:05:06:07:08", eui, err), "eui colon");
    ExpectEq(HexEncode(eui), "0102030405060708", "eui hex");
    Expect(!ParseGatewayEUI("01020304", eui, err), "eui short");
    Expect(!ParseGatewayEUI("zzzzzzzzzzzzzzzz", eui, err), "eui invalid");
  }

  {
    std::vector<api::GatewayRadio> radios;
    std::string err;
    Expect(ParseGatewayRadios("0:27,1:14:-130", radios, err), "radios ok");
    ExpectEq(radios.size(), size_t{2}, "radios len");
    ExpectEq(static_cast<int>(radios[0].rf_chain()), 0, "rf0");
    ExpectNear(radios[0].max_tx_power(), 27.0, 1e-3, "power0");
    Expect(!radios[0].has_gain_sensitivity(), "no sens0");
    ExpectEq(static_cast<int>(radios[1].rf_chain()), 1, "rf1");
    Expect(radios[1].has_gain_sensitivity(), "sens1");
    ExpectNear(radios[1].gain_sensitivity(), -130.0, 1e-3, "sens val");
    Expect(!ParseGatewayRadios("0:27,0:14", radios, err), "duplicate rf");
    Expect(!ParseGatewayRadios("bad", radios, err), "bad radios");
  }

  {
    Config c;
    c.gateway_id = "deadbeefdeadbeefdeadbeef";
    c.gateway_eid = "edge-1";
    c.gateway_eui = "0102030405060708";
    c.gateway_name = "Roof GW";
    c.gateway_location = "37.9,23.7,10";
    c.gateway_radios = "0:27";
    ParsedGatewaySync parsed;
    std::string err;
    Expect(ParseGatewaySyncConfig(c, parsed, err), "full parse");
    Expect(err.empty(), "full no err");
    auto req = ToUpsertRequest(parsed);
    ExpectEq(req.gateway_id(), "deadbeefdeadbeefdeadbeef", "req id");
    ExpectEq(req.external_id(), "edge-1", "req eid");
    ExpectEq(req.name(), "Roof GW", "req name");
    ExpectEq(HexEncode(std::vector<uint8_t>(req.gateway_eui().begin(), req.gateway_eui().end())),
             "0102030405060708", "req eui");
    Expect(req.has_location(), "req loc");
    Expect(req.has_radios(), "req radios");
    ExpectEq(static_cast<int>(req.radios().values_size()), 1, "req radios len");
  }

  return Summary();
}
