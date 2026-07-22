# forwarder-cc

C++ implementation of the Kudzu Analytics Forwarder. It sits between the Semtech UDP Packet Forwarder and the LoRaWAN server, forwarding traffic and pushing analytics to the Kudzu Analytics platform via the [client-cc](../client-cc) library.

## Features

- **Parity with Go forwarder**: Semtech UDP proxy, metrics aggregation per gateway, periodic flush, optional post-connect **gateway sync** (`GatewayUpsert`).
- **client-cc as sub-project**: Built via CMake `add_subdirectory`, no separate install of the client library.
- **Portability / small footprint**: No `-march=native`; C++17 + POSIX sockets (IPv4). No extra logging/test frameworks in the release binary. Use `MinSizeRel` / `-Os` on constrained devices.

## Dependencies

- **CMake** 3.14+
- **C++17** compiler (GCC, Clang)
- **gRPC** and **Protobuf** (same as [client-cc](../client-cc))
- **OpenSSL** (TLS and SHA1 dedupe)
- **nlohmann/json** (fetched by CMake via FetchContent)

## Build

```bash
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build
```

### Size-optimized

```bash
cmake -S . -B build-size -DCMAKE_BUILD_TYPE=MinSizeRel
cmake --build build-size
# optional: strip build-size/forwarder-cc
```

### Tests (not linked into the installed binary)

```bash
cmake -S . -B build-test -DFORWARDER_CC_BUILD_TESTS=ON -DCMAKE_BUILD_TYPE=Release
cmake --build build-test
ctest --test-dir build-test --output-on-failure
```

### Cross-compilation

```bash
cmake -S . -B build-kerlink \
  -DCMAKE_TOOLCHAIN_FILE=../basicstation-analytics/cmake/toolchain-kerlink.cmake \
  -DCMAKE_BUILD_TYPE=MinSizeRel
cmake --build build-kerlink
```

Point `Protobuf_ROOT` / `gRPC_ROOT` / `OPENSSL_ROOT_DIR` at the target sysroot when needed.

## Configuration

Same semantics as the [Go forwarder](../forwarder). Config file, environment variables, and command-line flags are **additive** (later overrides earlier):

1. **Defaults**
2. **Config file** — `--config=FILE` (INI `key=value`, `#` comments)
3. **Environment** — e.g. `CONNECT_HOST`, `CLIENT_ID`, `GATEWAY`, `GATEWAY_EUI`
4. **Command-line** — `--key=value`

| Option | Default | Description |
|--------|--------|-------------|
| `--connect-host` | (required) | LoRaWAN server hostname |
| `--client-id` | (required) | Analytics client ID |
| `--client-key` | (required) | Analytics client key (hex) |
| `--gateway` | (required if not server-side) | Gateway ObjectID (also used as `gateway_id` in upsert) |
| `--gateway-eid` | | External gateway ID for `GatewayUpsert` |
| `--gateway-eui` | | LoRaWAN EUI (16 hex chars; `-`/`:` allowed) |
| `--gateway-name` | | Display name for upsert |
| `--gateway-location` | | `lat,lon[,alt]` for upsert |
| `--gateway-radios` | | `rf_chain:max_tx_power[:tx_sensitivity][,...]` |
| `--analytics-endpoint` | `ingress.eu1.cluster.kudzu.gr:443` | Analytics gRPC endpoint |
| `--pairing-endpoint` | `https://console.eu1.cluster.kudzu.gr` | Pairing HTTPS base URL |
| `--analytics-ca-file` | | Optional CA PEM (additive to system trust) |
| `--analytics-ssl-target-name` | | TLS SNI / cert hostname override (C++-specific) |
| `--listen-host` | `127.0.0.1` | Bind address for UDP proxy |
| `--listen-port-up` | `1800` | Uplink listen port |
| `--listen-port-down` | `1801` | Downlink listen port (same as up → shared socket) |
| `--connect-port-up` / `--connect-port-down` | `1700` | LoRaWAN server ports |
| `--connect-retry-interval` | `1` | Seconds before rebinding local UDP sockets after recv errors |
| `--flush-interval` | 10 (client) / 5 (server) | Seconds between metric flushes |
| `--server-side` | `false` | Server-side multi-gateway mode |
| `--max-udp-streams` | 2 / 256 | Max gateway streams / metrics frames |
| `--log-level` | `info` | `debug` / `info` / `warn` / `error` |
| `--log-file` | | Optional log file (append); default stderr |
| `--debug-dump` | | Write `stream_id:base64_payload` lines for proxied traffic |
| `--pair-pin` | | Fetch config from pairing service and exit |
| `--write` | | With `--pair-pin`, write INI to `--config` path |
| `--config` | | INI config file path |
| `--version` | | Print version (`v0.1.12`) and git hash |

### Gateway sync

If any of `gateway-eid`, `gateway-eui`, `gateway-name`, `gateway-location`, or `gateway-radios` is set, sync is enabled and **`gateway-eid` or `gateway-eui` is required**. After analytics connect, the forwarder schedules a non-blocking `GatewayUpsert`. Failures are logged; UDP proxying continues. There is no automatic `GatewayDelete`.

### Pairing

```bash
./forwarder-cc --pair-pin=YOUR_PIN
./forwarder-cc --pair-pin=YOUR_PIN --write --config=/etc/kudzu-forwarder.conf
```

Default pairing endpoint: `https://console.eu1.cluster.kudzu.gr`. The client
appends `/api/v1/pairing/edge`. Override it with `--pairing-endpoint`, the
`pairing-endpoint` INI key, or `PAIRING_ENDPOINT`. Pairing never derives its URL
from the independent `analytics-endpoint` gRPC setting.

### Example

```bash
./forwarder-cc \
  --connect-host=eu1.cloud.thethings.network \
  --client-id=YOUR_CLIENT_ID \
  --client-key=YOUR_CLIENT_KEY \
  --gateway=YOUR_GATEWAY_ID \
  --gateway-eui=0102030405060708 \
  --gateway-name="Roof GW" \
  --listen-host=127.0.0.1 \
  --listen-port-up=1800 \
  --listen-port-down=1800
```

## License

Same as the analytics repository.
