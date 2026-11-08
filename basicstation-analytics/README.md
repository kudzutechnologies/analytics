# Basicstation Analytics Consumer

<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright 2026 Kudzu Technologies -->

Connects to a Basic Station analytics UNIX datagram socket and forwards telemetry
(uplinks, downlinks, gateway statistics) to Kudzu Analytics via gRPC.

## Protocol (v1, finalized)

- Host-endian packed structs shared by producer and consumer on the same machine.
- Header: magic `0xA7`, version `1`, type, reserved, endian marker `0x01020304`.
- `ANA_HELLO` carries gateway EUI (sent at open and every 30s with stats).
- Uplink/downlink: fixed metadata prefix + full LoRaWAN PHYPayload tail (max 255).
- Reject datagrams with wrong magic/version/endian, wrong exact length, or oversized payload.

## Requirements

- Basic Station built with analytics side-channel (`analytics_sock` / `STATION_ANALYTICS` / `-A`).
- CMake 3.14+, C++17, gRPC, Protobuf, OpenSSL (from `client-cc`).
- Kudzu gateway ObjectID (`--gateway`); do not use EUI as ObjectID.

## Building

```bash
cmake -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build
# Binary: build/basicstation-analytics
```

With tests:

```bash
cmake -B build -DCMAKE_BUILD_TYPE=Debug -DBS_ANALYTICS_BUILD_TESTS=ON
cmake --build build
ctest --test-dir build --output-on-failure
```

### Cross-compilation

Toolchains live under `cmake/toolchain-{corecell,rpi,kerlink}.cmake`. Target
sysroots must provide gRPC, Protobuf, OpenSSL, and pthreads. Missing target
dependencies are release blockers.

```bash
cmake -B build-corecell -DCMAKE_TOOLCHAIN_FILE=cmake/toolchain-corecell.cmake -DCMAKE_BUILD_TYPE=Release
cmake --build build-corecell
```

Same pattern for `toolchain-rpi.cmake` and `toolchain-kerlink.cmake`. Override
roots with `-DTOOLCHAIN_ROOT=...`.

## Startup order

1. Create `/run/basicstation` (tmpfiles / systemd `RuntimeDirectory`).
2. Start **this bridge** so it binds the UNIX socket.
3. Start Basic Station with the same path (or restart it after the bridge).

HELLO is resent every 30s, so a late station reconnect recovers identity without
restarting forever — but first packets may queue until HELLO arrives.

## Permissions / threat model

- UNIX DGRAM path is local and cleartext (includes PHYPayload).
- Use a private directory (`0750`) and socket mode (`0660` default).
- Any local process that can write to the bound path can inject traffic.

## Configuration

Precedence: defaults → INI (`--config`) → environment → CLI.

Required:

- `--socket` / `ANALYTICS_SOCK`
- `--client-id` / `CLIENT_ID`
- `--client-key` / `CLIENT_KEY`
- `--gateway` (ObjectID)

Optional:

- `--endpoint` (default `ingress.eu1.cluster.kudzu.gr:443`)
- `--ca-file`, `--ssl-target-name`
- `--batch-sec` (default 10), `--max-batches` (default 32)
- `--gateway-eid`, `--gateway-eui`, `--gateway-name`, `--gateway-location`, `--gateway-radios`
- `--pair-pin` / `--write` for console pairing
- `--pairing-endpoint` (default `https://console.eu1.cluster.kudzu.gr`)
- `--log-level`, `--log-file`

Pairing uses `pairing-endpoint` as an HTTPS base URL and appends
`/api/v1/pairing/edge`. It can be overridden by the `PAIRING_ENDPOINT`
environment variable or `--pairing-endpoint`; it is independent from the
`analytics-endpoint` gRPC ingress setting.

Example:

```bash
mkdir -p /run/basicstation
./basicstation-analytics \
  -s /run/basicstation/analytics.sock \
  -i YOUR_CLIENT_ID -k YOUR_CLIENT_KEY \
  --gateway YOUR_OBJECT_ID \
  -e ingress.eu1.cluster.kudzu.gr:443 -b 10
```

## Delivery semantics

- Ingest and gRPC send are separated by a bounded in-memory queue.
- On send failure the current batch is retained and retried with backoff.
- When the queue is full the oldest batch is dropped (logged + counters).
- Shutdown drains with `--shutdown-deadline-ms` (default 5000).

## Packaging

- systemd + tmpfiles: `packaging/systemd/`
- Kerlink SysV IPK assets: `packaging/kerlink/` (start S53 so the bridge binds before station)

## License

MIT — same as the analytics repository (Copyright 2026 Kudzu Technologies).
