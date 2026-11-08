# Basic Station analytics consumer

This C++ daemon reads a local Unix datagram analytics side channel and sends
uplinks, downlinks, and statistics to Kudzu Analytics. It does not sit in the
radio forwarding path. Delivery uses a bounded memory queue and a dedicated
sender owning its gRPC client. There is no telemetry spool.

## Producer prerequisite

The custom Basic Station producer source is not in this repository. Its exact
commit, header, build, and real datagram fixtures must be supplied before release.
The consumer's `protocol.h` describes the expected packed host-endian v1 layout:
magic 0xA7, version 1, type, reserved, and endian marker 0x01020304. HELLO carries
a nonzero 64-bit EUI; fixed radio prefixes are followed by a PHYPayload of at most
255 bytes. Wrong lengths, endian markers, and conflicting HELLO identities are
rejected. The analytics gateway ObjectID must be configured separately.

Do not treat consumer-built fixtures as proof of compatibility with a deployed
producer. Run the ABI tool with the pinned producer header and compiler:

```sh
python3 tools/check-basicstation-abi.py --producer-header /sdk/station/src/analytics.h \
  --producer-ref FULL_40_CHARACTER_COMMIT_SHA --cxx /sdk/bin/target-g++ \
  --sysroot /sdk/sysroot --include-dir /sdk/station/include
```

## Build

Requirements: CMake 3.24+, C++17, Protobuf, gRPC, OpenSSL, and pthreads. A native
host `protoc` and `grpc_cpp_plugin` must match the target libraries. Bindings are
generated into the build tree; checked-in historical bindings are not compiled.
The nlohmann JSON download is pinned by SHA256.

```sh
cmake -S basicstation-analytics -B /tmp/bs-build -DCMAKE_BUILD_TYPE=MinSizeRel \
  -DBS_ANALYTICS_BUILD_TESTS=ON -DCLIENT_CC_BUILD_TESTS=ON
cmake --build /tmp/bs-build -j2
ctest --test-dir /tmp/bs-build --output-on-failure
```

For Corecell, RPi, and Kerlink use `cmake/toolchain-{corecell,rpi,kerlink}.cmake`
and explicitly set `TOOLCHAIN_ROOT`, `CMAKE_SYSROOT`, `CLIENT_CC_HOST_PROTOC`, and
`CLIENT_CC_HOST_GRPC_PLUGIN`. The sysroot must include target gRPC, Protobuf,
OpenSSL, and matching CMake package metadata. Host libraries must not enter the
target link. See [sdk-manifest.example.json](cmake/sdk-manifest.example.json) and
[release verification](../docs/basicstation-release.md). Missing SDKs are a
release blocker, not a successful cross build.

## Provisioning and startup

Create a private runtime directory owned by the daemon and shared only with the
Station producer (`0750`, or stricter). Run the consumer before Station, both
using the same socket path. A cooperating producer must resend HELLO after a
consumer restart. The claimed periodic HELLO behavior must be verified against
the supplied producer source and target device.

```ini
# /etc/kudzu-bs-analytics.conf (0600)
socket=/run/basicstation/analytics.sock
socket-mode=0660
client-id=YOUR_ANALYTICS_CLIENT_ID_HEX
client-key=YOUR_ANALYTICS_CLIENT_KEY_HEX
gateway=YOUR_KUDZU_GATEWAY_OBJECT_ID
```

Run `basicstation-analytics --config=/etc/kudzu-bs-analytics.conf`. The consumer
holds a process lock at `socket-path.lock`, leaves a zero-byte lock file for
safe restarts, refuses to replace live sockets or regular files, and removes
only its own socket inode. The parent must already exist with private
permissions. Socket contents are cleartext; any process granted access can
inject telemetry. If `gateway-eui` is configured, HELLO must match it.

## Configuration

Precedence: defaults → INI → environment → CLI. Kebab-case keys map to uppercase
underscore variables; `socket` specifically maps to `ANALYTICS_SOCK`.
`--socket/-s`, `--client-id/-i`, `--client-key/-k`, and `--gateway` are required.
`--config/-c` / `CONFIG` selects a file. Invalid limits, conflicting aliases,
unknown keys, and incomplete gateway synchronization metadata fail startup.

| Control | Default | Minimum / meaning |
|---|---:|---|
| `max-batches` | 2 | 1; partial, retrying, and in-flight combined |
| `max-messages` | 32 | 1 per batch, including stats |
| `max-buffer-bytes` | 65536 | 1024; serialized records plus conservative overhead |
| `pre-identity-max-messages` | 0 | 0; drop until HELLO by default |
| `socket-rcvbuf-bytes` | 16384 | 1024 requested kernel receive bytes |
| `poll-max-datagrams` | 32 | 1; bounded work per poll |
| `batch-sec/-b` | 10 | 1 second; `FLUSH_INTERVAL` alias |
| `shutdown-deadline-ms` | 5000 | 0; drain/cancel/join share one deadline |
| `analytics-connect-timeout` | 5 | 1 second |
| `analytics-request-timeout` | 5 | 1 second |
| `analytics-max-backoff` | 60 | 1 second; exponential retry cap |

Use [packaging/minimum.env](packaging/minimum.env) to retain one message in one
batch with a 1024-byte charge limit. Process RSS also includes gRPC, TLS,
allocator and protobuf overhead. Kernel receive limits are adjusted by the OS;
the effective value is logged. Byte limits are not hard RSS caps.

A pinned in-flight item consumes queue capacity. Oldest waiting items are evicted
under pressure; arrivals are dropped if all capacity is in flight. Failed
pushes stay queued until success, eviction, INVALID_ARGUMENT, or process exit.
Optional pre-HELLO retention is also bounded and never published without
identity. Nonzero retained telemetry at shutdown yields exit code 2. Repeated
RPCs may produce duplicates; restart/deadline expiry may lose data.

`analytics-endpoint` / `--endpoint/-e` defaults to
`ingress.eu1.cluster.kudzu.gr:443`. `analytics-ca-file` / `--ca-file` adds trust;
`analytics-ssl-target-name` / `--ssl-target-name` overrides the TLS name.
`gauge-stat` defaults false. Logs default to stderr at info; minute/shutdown
summaries expose retained counts/bytes, rejection, loss reasons, and failures.
`log-file` is opt-in and must be rotated by the operator. No metrics listener or
application log file is created by default.

Optional gateway synchronization uses `gateway-eid`, `gateway-eui`,
`gateway-name`, `gateway-location=lat,lon[,alt]`, and
`gateway-radios=rf_chain:max_tx_power[:tx_sensitivity],...`. Any metadata requires
an EUI or external ID. Coordinates/powers must be finite. This managed task owns
a separate client and is cancelled/joined on shutdown.

`--pair-pin=PIN` prints a configuration. Add `--write --config=PATH` to write it
with mode 0600. `pairing-endpoint` / `PAIRING_ENDPOINT` defaults to
`https://console.eu1.cluster.kudzu.gr`; `/api/v1/pairing/edge` is appended.
Analytics and pairing endpoints are independent. `--version` prints the release
version and revision.

## Packages and services

Kerlink package builds require an actual ARM32 little-endian target executable
and runtime dependency names from the SDK:

```sh
make -C basicstation-analytics/packaging/kerlink \
  BINARY=/tmp/build-kerlink/basicstation-analytics VERSION=0.1.0 \
  RUNTIME_DEPENDS='SDK_SPECIFIC_RUNTIME_DEPENDENCIES'
```

The deterministic IPK contains executable service/hooks and private configs;
conffiles preserve local settings on upgrade. It is disabled until configured.
No firewall rules are modified. The init script checks the PID's executable
before sending TERM and lets the application's shutdown deadline govern exit.
Systemd assets use a runtime directory and preserve it across restart. Operator
shutdown deadlines should fit the device's service supervision policy.
Structural archive tests do not certify opkg installation or target runtime
libraries: install/start/stop/upgrade/radio/outage tests remain required on each
target. See [release status](../docs/basicstation-release-status.json).
