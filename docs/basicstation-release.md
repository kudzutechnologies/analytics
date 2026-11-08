# Basic Station release verification

The original WIP `d84cf3f72e4ee8e92493cfcbd0b19e90bb99892d` is preserved at
`review-wip-production-219b30f6` and `backup/basicstation-wip-d84cf3f`.
`ic/feat/basicstation` replaces it with ten component commits, followed by
incremental build, configuration, delivery, shutdown, authorization, routing,
CUPS, radio, socket, packaging, and release-check changes. The reconstructed
component tip `5319e7e6cbffbb6bd446a32a1580a0304d53f3f2` has exactly the original
WIP tree (`4105d6e6c01b087fb05ab5185b9a2054fe7b22b4`). Hardening commits follow
that point. No branch has been pushed or release tagged by this work.

The authoritative gate inventory is [basicstation-release-status.json](basicstation-release-status.json).
A native test pass is not a release approval for embedded targets.

## Migration from the WIP

1. Provision analytics credentials and a real analytics gateway ObjectID.
2. Choose local loopback mode or shared TLS mode. Shared deployments must supply
   a listener identity, public root URI, and a pre-provisioned gateway registry.
   Default binding is now loopback, no certificate directory is implicitly
   created, CUPS is off, and unknown gateways cannot enroll themselves.
3. Import upstream TC and CUPS credentials separately. Do not reuse ingress
   tokens for upstream access. Explicitly allow every legitimate assigned LNS
   origin. Keep upstream verification enabled.
4. For managed CUPS, provision the proxy CA and matching explicit listener
   certificate first; verify Station trust and every authorized router. Check
   initial update, repeat no-op CRC response, distinct TC/CUPS credentials,
   certificate rotation, and signed firmware updates against the real server.
5. For the socket consumer, provision a private runtime parent and configure
   matching socket paths. Verify the pinned producer sends HELLO and that the
   expected EUI matches. Data before identity is dropped by default.
6. Review the new small queue defaults (2 batches × 32 messages, 65536 charged
   bytes). Source the component's `minimum.env` when one batch/one message is
   appropriate. These limits do not cap process RSS. Choose finite timeouts
   and a shutdown deadline appropriate to the supervisor and device.
7. Install candidate packages disabled, populate private configuration, enable,
   and verify radio forwarding, outage behavior, clean restart and upgrade.
   Retained telemetry is lost on restart; delivery may duplicate after an
   ambiguous RPC. No disk spool is supported.

See [Go proxy usage](../forwarder-bs/README.md) and
[C++ consumer usage](../basicstation-analytics/README.md) for exact settings.

## Reproducible local checks

```sh
go test ./...
go vet ./...
go test -race ./client ./forwarder-bs
go test -race ./forwarder # inherited failing release gate; see status
ANALYTICS_SLOW_TESTS=1 go test ./forwarder-bs -run '^TestLongIdleConnection$' -count=1 -timeout=100s
go test ./forwarder-bs -run '^$' -fuzz=FuzzCUPSResponse -fuzztime=10s -parallel=2
python3 -m unittest discover -s tools/tests -v
python3 tools/build-go-forwarders.py --component bs --version dev --output-dir /tmp/artifacts
python3 tools/build-go-forwarders.py --component udp --version dev --output-dir /tmp/artifacts
cmake -S basicstation-analytics -B /tmp/bs-build -DCMAKE_BUILD_TYPE=MinSizeRel \
  -DBS_ANALYTICS_BUILD_TESTS=ON -DCLIENT_CC_BUILD_TESTS=ON
cmake --build /tmp/bs-build -j2
ctest --test-dir /tmp/bs-build --output-on-failure
cmake -S forwarder-cc -B /tmp/udp-build -DFORWARDER_CC_BUILD_TESTS=ON
cmake --build /tmp/udp-build -j2
ctest --test-dir /tmp/udp-build --output-on-failure
```

Configure separate Debug builds with `-DCMAKE_CXX_FLAGS=-fsanitize=thread`, or
`-DCMAKE_CXX_FLAGS=-fsanitize=address,undefined` (and matching linker flags).
Use sanitizer-compatible dependency builds. The initial local ASan run failed
inside Homebrew Protobuf 36.2's has-bit consistency checker. An isolated
program constructing/destroying only an empty generated AnalyticsMetrics message
reproduced it. That installed header enables `PROTOBUF_MESSAGE_GLOBALS` under
ASan, which differs from the installed unsanitized runtime. Rebuilding the
application with the header's `-DPROTOBUF_MESSAGE_GLOBALS_TEMPORARY_OPTOUT`
compatibility switch passed all 13 tests with ASan and UBSan still enabled.
This local workaround is not applied to production builds or CI. Prefer matching
instrumented dependencies for full dependency coverage; record this limitation
when assessing the local pass. CI runs these checks independently and keeps the inherited Go
UDP race gate visible, without `continue-on-error`.

The slow proxy test holds an actual fixture WebSocket idle for over 60 seconds,
then checks forwarding. Unit/fixture coverage also includes cancellation,
retry and in-flight queue accounting, admission/auth isolation, unsigned 64-bit
router IDs, routing replacements, CUPS CRC/no-op/streaming behavior, malformed
credential lengths, shared radio goldens, and socket ownership. Tests use dummy
local servers and credentials; no production service is exercised.

## Native resources and target budgets

[Measured results](basicstation-resource-measurements.json) are native host
observations, not device budgets. Reproduce with stripped/optimized candidate
binaries:

```sh
go build -trimpath -ldflags='-s -w' -o /tmp/kudzu-forwarder-bs ./forwarder-bs
python3 tools/resource-smoke.py --go-binary /tmp/kudzu-forwarder-bs \
  --cpp-binary /tmp/bs-build/basicstation-analytics \
  --seconds 30 --output /tmp/basicstation-resources.json
```

The fixture sends unique radio payloads through a local WebSocket or host-endian
socket, makes the backend unavailable, samples RSS, checks overflow counters,
measures 100 ms shutdown, and inventories application runtime files. It uses
1-second flushes and captures metadata logs in a bounded pipe. No app telemetry
or log files are permitted in this profile. Only the consumer's empty runtime
lock survives. The C++ executable size excludes dynamically linked libraries;
include their full installed closure when measuring target disk use. RSS
sampling can miss short peaks; use target tools for high-water measurements.

On each physical target run idle, sustained normal traffic, sustained outage,
recovery, repeated reconnects, firmware/CUPS updates where applicable, and
repeated process restarts for default and minimum profiles. Record RSS/high
water, kernel socket memory, binary plus dependency bytes, file growth and
writes, drops, recovery and shutdown timing. Agree budgets against actual device
headroom. A 1024-byte telemetry budget does not imply 1024-byte process memory.

## Embedded candidate and release gates

Provide these four environment variables:

- `BASICSTATION_SDK_MANIFEST`: copy and fill
  [sdk-manifest.example.json](../basicstation-analytics/cmake/sdk-manifest.example.json)
  for Corecell, RPi, and Kerlink. Pin SDK/runtime versions and provide target
  compiler/sysroot, executable matching host protoc/plugin, and runtime package
  dependencies. Do not substitute host libraries.
- `BASICSTATION_PRODUCER_HEADER`: exact header from the custom producer source.
- `BASICSTATION_PRODUCER_REF`: its full 40-character commit SHA. Review source and
  capture real HELLO/uplink/downlink/stat datagrams on every target. The ABI
  comparator checks layout/constants; it cannot prove semantics or provenance.
- `BASICSTATION_HARDWARE_EVIDENCE`: JSON object keyed by `corecell`, `rpi`, and
  `kerlink`. Each value requires `producer_ref`, `binary_sha256`, `passed: true`,
  and `checks` with true values for `install`, `start`, `stop`, `upgrade`,
  `radio`, `minimum-memory`, and `outage`. Retain the detailed test records with
  operator/device/SDK metadata alongside this summary.

Build and test candidates with the documented toolchain and the final intended
version. Hardware evidence must name the SHA256 of the stripped executable.
The release rebuild must reproduce that exact candidate. Also install Docker
and the separately supplied Kerlink `gen_remove_ipk` utility for the existing
UDP package/removal artifacts. This tool is not vendored here.

`python3 tools/release-basicstation.py --check-inputs` validates embedded inputs
without requiring a tag. `make release-check` additionally requires a clean
checkout on an exact semantic version tag and legacy package tools. `make
release` performs target ABI checks, cross builds, runtime version checks,
hardware-candidate hash comparison, all Go target builds, C++ Kerlink IPK, and
existing UDP package/removal builds. Any failure is fatal. Output goes to
`releases/TAG` with manifests and SHA256SUMS; source files are not rewritten.
No upload, tagging, or publication is performed by these commands.

Do not mark release ready while an external gate, sanitizer compatibility
question, inherited race, or required device check remains unresolved. Update
the status JSON with evidence after each gate is actually satisfied.
