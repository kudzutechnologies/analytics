#!/usr/bin/env python3
"""Compare producer and consumer v1 layouts without executing target code."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--producer-header',type=Path,required=True)
parser.add_argument('--producer-ref',required=True,help='Pinned producer commit')
parser.add_argument('--cxx',default='c++')
parser.add_argument('--include-dir',type=Path,action='append',default=[])
a=parser.parse_args()
if not a.producer_header.is_file():parser.error('producer header is unavailable; ABI gate is blocked')
if len(a.producer_ref)!=40 or any(c not in '0123456789abcdefABCDEF' for c in a.producer_ref):parser.error('producer-ref must be a full commit SHA')
consumer=Path(__file__).resolve().parent.parent/'basicstation-analytics/protocol.h'
fields={
 'ana_hdr':['magic','version','type','reserved','endian'],
 'ana_hello':['hdr','gateway_eui','station_ver'],
 'ana_uplink':['hdr','rx_wall_us','rx_gps_time','xtime','freq','rssi','snr_x4','fts','dr','sf','bw','crc_status','size'],
 'ana_downlink':['hdr','tx_time_us','tx_gps_time','freq','txpow_x10','dr','sf','bw','txflags','txunit','addcrc','preamble','size'],
 'ana_stat':['hdr','gw_time_us','latitude','longitude','altitude','rx_packets','rx_ok','rx_forwarded','tx_received','tx_emitted','is_gauge','reserved','temperature'],
}
constants=['ANA_MAGIC','ANA_VERSION','ANA_ENDIAN','ANA_MAX_PAYLOAD']
renamed=list(fields)+['ana_hdr_ok','ANA_UPLINK','ANA_DOWNLINK','ANA_STAT','ANA_HELLO']
lines=['#include <cstddef>']+[f'#define {name} producer_{name}' for name in renamed]
lines += ['#include '+json.dumps(str(a.producer_header.resolve()))]
lines += [f'constexpr auto producer_constant_{c}={c};' for c in constants]
lines += [f'#undef {name}' for name in renamed+constants+['BASICSTATION_ANALYTICS_PROTOCOL_H','ANA_UPLINK_LEN','ANA_DOWNLINK_LEN']]
lines += ['#include '+json.dumps(str(consumer))]
for name,members in fields.items():
 lines.append(f'static_assert(sizeof({name})==sizeof(producer_{name}),"{name} size mismatch");')
 for field in members:lines.append(f'static_assert(offsetof({name},{field})==offsetof(producer_{name},{field}),"{name}.{field} offset mismatch");')
lines += [f'static_assert({c}==producer_constant_{c},"{c} mismatch");' for c in constants]
with tempfile.TemporaryDirectory(prefix='basicstation-abi-') as directory:
 source=Path(directory)/'layout.cc';source.write_text('\n'.join(lines)+'\n')
 subprocess.run([a.cxx,'-std=c++17','-fsyntax-only',str(source),*[v for p in a.include_dir for v in ('-I',str(p))]],check=True)
print(json.dumps({'layout_verified':True,'producer_ref':a.producer_ref,'producer_header_sha256':hashlib.sha256(a.producer_header.read_bytes()).hexdigest(),'radio_semantics_and_producer_fixtures':'still require validation'},indent=2))
