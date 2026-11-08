# Kudzu Analytics Basic Station Forwarder

> WebSocket proxy for LoRa Basic Station gateways — extract LoRaWAN analytics without changing LNS or gateway logic.

This package provides a **man-in-the-middle** proxy for the [LoRa Basic Station](https://github.com/lorabasics/basicstation) LNS protocol. It sits between Basic Station (on the gateway) and your LoRaWAN Network Server (LNS). Traffic is forwarded unchanged; uplinks and downlinks are inspected to collect analytics and push them to Kudzu Analytics via the analytics client library.

![Architecture](https://kudzu.gr/images/solutions/analytics-infra-1.svg)

## How it works

1. **Discovery**  
   The gateway connects to the forwarder’s `/router-info` WebSocket. The forwarder relays the request to the real LNS and rewrites the response so the gateway is told to connect to the forwarder for data (instead of the LNS).

2. **Data connection**  
   The gateway opens a WebSocket to the forwarder. The forwarder opens a WebSocket to the real LNS and bridges messages both ways. All frames are forwarded as-is.

3. **Analytics**  
   The forwarder parses JSON messages (`jreq`, `updf`, `propdf`, `dnmsg`, etc.), maps them to the analytics data model (uplinks, downlinks, DR table from `router_config`), and periodically flushes batches to the analytics backend.

Basic Station documentation: [doc.sm.tc/station](https://doc.sm.tc/station/).

## Installation

Use pre-built binaries from the [Releases](https://github.com/kudzutechnologies/analytics/releases) when available, or build from source (see [Build instructions](#build-instructions)).

Example (ARM7, Linux):

```sh
curl -L -o kudzu-forwarder-bs.tgz https://github.com/kudzutechnologies/analytics/releases/download/v0.1.0/kudzu-forwarder-bs-arm7.tgz
tar -zxf kudzu-forwarder-bs.tgz
mv kudzu-forwarder-bs-arm7 /usr/bin/kudzu-forwarder-bs
```

## Configuration

Configuration can be provided via a config file (e.g. `/etc/kudzu-forwarder-bs.conf`), environment variables, or command-line flags. The forwarder uses the same analytics credentials and concepts as the [Semtech UDP forwarder](../forwarder/README.md).

### Example config file

```ini
# ======================================================
# Kudzu Analytics Basic Station Forwarder
# ======================================================

# From Kudzu Analytics (shared per customer)
client-id="<api-client-id>"
client-key="<api-client-key>"

# From Kudzu Analytics, unique per gateway (required when not server-side)
gateway="<platform-gateway-id>"

# Upstream LNS WebSocket URI (data endpoint)
connect-uri=wss://eu1.cloud.thethings.network:8887

# Where the forwarder listens (Basic Station will connect here after discovery)
listen-host=0.0.0.0
listen-port=8090

# Optional: TLS for gateway connections (wss://)
# listen-tls-cert=/path/to/cert.pem
# listen-tls-key=/path/to/key.pem
# Or use certs-dir: forwarder creates bootstrap cert and per-gateway CUPS client certs (stations must trust certs-dir/ca.crt)
# certs-dir=./certs

# Optional: verify upstream LNS (e.g. custom CA)
# connect-tls-ca=/path/to/ca.pem
# connect-tls-insecure=false

# Logging
log-level=info
# log-file=/var/log/kudzu-forwarder-bs.log
```

### Running the forwarder

```sh
/usr/bin/kudzu-forwarder-bs --config=/etc/kudzu-forwarder-bs.conf
```

### Pointing Basic Station at the forwarder

Basic Station learns the LNS endpoint from **tc.uri**. Configure the gateway so that **tc.uri** points at the forwarder, not the LNS.

- **Discovery**: Basic Station connects to `tc.uri` + `/router-info`. The forwarder uses a single upstream discovery URL: **connect-discovery-uri** if set, otherwise **connect-uri** + **connect-discovery-path** (default `/router-info`). The LNS response includes **router**, **muxs**, and **uri** (the LNS data WebSocket URL for that gateway). The forwarder **stores this full LNS-allocated config** per gateway (in **certs-dir**, as `{gatewayID}.lns_config`) and rewrites the response so the station is told to connect to the forwarder for data. The forwarder does not rely on config file for the LNS data endpoint when the LNS has provided it via router-info. If the upstream returns **404** (e.g. ThingPark Community does not expose router-info), the forwarder responds with a **synthetic** router-info and no LNS config is stored for that gateway.
- **Data**: When the station opens the data connection to the forwarder, the forwarder identifies the gateway by **stable identity only**: the `router` query parameter or the TLS client certificate (Subject CN, or a match against stored certs). It does **not** use connection endpoint (RemoteAddr). It then looks up that gateway’s **stored** LNS config (data URI from router-info). If present, it uses that URL for the upstream connection; otherwise it falls back to **connect-uri** + **connect-data-path** (or **connect-data-uri**) from config. Certificates and config files are keyed by this stable gateway ID (e.g. normalized router or client cert CN), so filenames like `{gatewayID}.crt` remain stable across reconnects.

So set **tc.uri** to the forwarder’s WebSocket base URL, for example:

- Without TLS: `ws://forwarder-host:8090`
- With TLS: `wss://forwarder-host:8090`

If the forwarder runs on the same host as Basic Station:

```ini
# In station config (e.g. tc.uri file or station.conf)
tc.uri=ws://127.0.0.1:8090
```

Credentials (tc.trust, tc.crt, tc.key) are still used by Basic Station to connect to the forwarder; the forwarder uses the same credentials (or its own) to connect to the real LNS when you use TLS.

### Connecting to Actility ThingPark

ThingPark Community (`community.thingpark.io`) and ThingPark Enterprise use TLS signed by **Amazon Trust Services**. The following is a complete setup so that Basic Station talks to the forwarder and the forwarder talks to ThingPark.

#### Certificates

- **ThingPark server (upstream)**  
  Actility’s CUPS and LNS use certificates that chain to **Amazon Root CA 1**. You must verify the upstream with this CA.

  - **Download**: [Amazon Root CA 1 (PEM)](https://www.amazontrust.com/repository/AmazonRootCA1.pem). Save it (e.g. `AmazonRootCA1.pem`).

- **Forwarder (proxy)**  
  The forwarder terminates TLS for the station. Use either:
  - **listen-tls-cert** / **listen-tls-key**: your own certificate (e.g. for your hostname), **or**
  - **certs-dir** without listen certs: the forwarder creates a bootstrap cert signed by its CA (`ca.crt` / `ca.key` in certs-dir). The station must then trust that CA.

#### Basic Station (gateway) configuration

- **cups.uri**  
  - If CUPS goes **through the forwarder**: set `cups.uri` to the forwarder’s HTTPS URL (e.g. `https://forwarder-host:8090` if the forwarder serves TLS on 8090).  
  - If CUPS goes **directly to ThingPark**: set `cups.uri=https://community.thingpark.io:443`.

- **cups.trust** (where to put Actility’s CA)  
  This file must contain the PEM of the CA that signed the server you connect to for CUPS:
  - **CUPS via forwarder**: put the **forwarder’s** CA (the one that signed the forwarder’s listen certificate). If you use certs-dir, use `certs-dir/ca.crt`. Path on the gateway is whatever your Basic Station config uses (e.g. `cups.trust=/etc/basicstation/cups.trust` pointing to a file that contains that PEM).
  - **CUPS directly to ThingPark**: put **Amazon Root CA 1** in `cups.trust` (same file path concept: the path is in the station config; the file content is the PEM you downloaded).

- **tc.uri**  
  Set to the forwarder’s WebSocket URL, e.g. `wss://forwarder-host:8090` or `ws://127.0.0.1:8090` if the forwarder is on the same host.

- **tc.trust**  
  The CA that signed the forwarder’s TLS certificate (so the station trusts the proxy). If you use certs-dir, use `certs-dir/ca.crt`. Same idea: station config points to a path; that file contains the PEM.

- **tc.crt / tc.key**  
  Optional client certificate for the station. The forwarder can work with or without them; for ThingPark upstream the forwarder uses server-issued or proxy-issued client certs as configured.

#### Forwarder configuration (for ThingPark)

Use explicit parameters; no automatic URL fallbacks.

| Parameter | Value | Notes |
|-----------|--------|------|
| **connect-uri** | `wss://community.thingpark.io:443` | Base URL for ThingPark (host + port). |
| **connect-data-path** | `/tpe/lns` | Path for LNS WebSocket on the same host as **connect-uri**. ThingPark Community may return 404 for `/tpe/lns`; if you see `data upstream handshake failed … HTTP 404`, try **connect-data-uri** with the correct LNS URL (see below) or another path (e.g. `/lns`) per Actility docs. |
| **connect-data-uri** | *(leave empty)* | Optional full LNS data WebSocket URI. When set, overrides connect-uri + connect-data-path. Use if LNS is on a different port (e.g. ThingPark Enterprise uses 4443: `wss://your-tpe-host:4443`) or when connect-data-path returns 404. |
| **connect-discovery-uri** | *(leave empty)* | Full router-info WebSocket URI. When set, used as-is. When empty and **connect-data-uri** is set, discovery uses the data URI host + **connect-discovery-path** (e.g. `wss://lns.thingpark.com:443/router-info`), so the correct endpoint is tried before falling back to synthetic router-info. |
| **connect-discovery-path** | *(default)* | Path for router-info on the discovery host; default `/router-info`. Used with **connect-uri** or, when **connect-data-uri** is set, with the data URI host. |
| **connect-tls-ca** | Path to `AmazonRootCA1.pem` | Required to verify ThingPark’s TLS. |
| **connect-ws-subprotocol** | `lora.v2` | ThingPark expects this subprotocol for the data WebSocket. |
| **certs-upstream-client-cert** | `false` | ThingPark does not trust your proxy CA; use server-issued or proxy-issued client cert only as sent by the forwarder. |
| **listen-tls-cert** / **listen-tls-key** | Your proxy cert | Or use **certs-dir** and have the station trust `certs-dir/ca.crt`. |

Example snippet for a config file:

```ini
connect-uri=wss://community.thingpark.io:443
connect-data-path=/tpe/lns
connect-tls-ca=AmazonRootCA1.pem
connect-ws-subprotocol=lora.v2
certs-upstream-client-cert=false
```

See **conf.local** in this repo for a full example.

### TLS and the certificate chain (CUPS / TC)

The forwarder **terminates TLS** on its listen side. So when the Station connects for **TC** (WebSocket) or **CUPS** (HTTPS), it only ever sees the **proxy’s** certificate, not the real LNS/CUPS server’s. That can break the chain and cause “Interaction with CUPS failed” or TC connection failures if the Station doesn’t trust the proxy.

**Option 1 – Trust the proxy on the Station (recommended if both TC and CUPS go through the proxy)**  
Use a certificate for the forwarder that the Station is configured to trust:

- Put the **CA that signed the forwarder’s listen certificate** into **tc.trust** and **cups.trust** on the Station, **or**
- Use a certificate for the forwarder that is signed by the same CA the real LNS/CUPS use (so the Station’s existing tc.trust / cups.trust already trust it).

Then the Station will accept the proxy’s certificate for both TC and CUPS.

**Per-gateway certificates and certs-dir**  
When **certs-dir** is set (default: `./certs`), the forwarder acts as a **private CA** and manages certificates in that directory:

- **CA**: `ca.crt` and `ca.key` — created on first run (or use **certs-ca-cert** / **certs-ca-key** to supply your own CA).
- **Bootstrap server cert**: If you do **not** set **listen-tls-cert** and **listen-tls-key**, the forwarder creates `bootstrap.crt` and `bootstrap.key` in certs-dir (signed by the forwarder CA) and uses them for incoming TLS (Station → proxy). **Stations must trust the forwarder CA**: put `ca.crt` from certs-dir into **cups.trust** and **tc.trust** on the Station.
- **Server-issued certs (recommended)**: During the **first** CUPS `POST /update-info`, the station has no client cert (server auth only). The proxy forwards the request to the real CUPS; the **server responds with a binary blob** that includes the gateway’s credentials (trust + cert + key). The proxy **intercepts this response**, extracts the cert and key, and stores them on disk (e.g. `{gatewayID}.server.crt`, `{gatewayID}.server.key`). On **subsequent** CUPS requests, the proxy uses these **server-issued** certs to authenticate to the upstream, so the upstream sees the connection as that gateway. The station always talks to the proxy with **your** listen cert (bootstrap or `listen-tls-cert`); the proxy uses the server-issued cert only for proxy→upstream.
- **certs-upstream-client-cert** (default `false`): When `false`, the first CUPS request to upstream uses server-auth only; the proxy then stores any server-issued cert from the response for later use. Set to `true` only when **your** upstream LNS/CUPS is configured to trust the proxy’s CA for client auth (e.g. your own ChirpStack or custom LNS where you add the proxy’s `ca.crt` as a trusted client CA). For **third-party** upstream (ThingPark, TTN, etc.) keep it `false`—they do not trust your proxy CA, so using our-issued cert would be rejected. The LNS WebSocket (router-info / data) always presents a client cert when possible (server-issued or, if none, proxy-issued) so upstream sees a consistent gateway identity.

**Option 2 – Send CUPS directly to the real server**  
Point **cups.uri** at the **real** CUPS server (not the forwarder). Point **tc.uri** at the forwarder. Then:

- CUPS traffic goes Station → real CUPS (TLS chain unchanged).
- Only TC (LNS) traffic goes through the forwarder; the Station only needs to trust the proxy for **tc.trust** when using `wss://`.

**“No TC URI configured”**  
If the Station reports “No TC URI configured”, it has no **tc.uri** set. Set **tc.uri** to the forwarder (e.g. `wss://forwarder-host:8090` or `ws://127.0.0.1:8090`) so the Station uses the proxy for LNS. CUPS can still go either through the proxy (Option 1) or directly to the real CUPS (Option 2).

**LNS closes the data connection right after connect**  
If the Station shows "Recv failed: SSL - The peer notified us that the connection is going to be closed", the upstream LNS is closing the WebSocket. The forwarder logs the LNS close code and reason (e.g. `LNS closed connection: code=1008 reason="..."`). Common causes: (1) ThingPark Community with null CUPS — the proxy then uses proxy-issued client certs; ThingPark may reject them. Ensure the gateway is registered and CUPS can return server-issued credentials. (2) Wrong connect-data-uri or connect-data-tls-server-name. (3) The first upstream ping is delayed 2s so the version message is sent first; check the logged close code/reason for details.

## Configuration options

| Option | Required | Default | Description |
|--------|----------|---------|-------------|
| **client-id** | yes* | `""` | Analytics API client ID (from Kudzu Analytics). |
| **client-key** | yes* | `""` | Analytics API client key. |
| **connect-uri** | yes | `""` | Upstream LNS WebSocket URI for data (e.g. `wss://lns.example.com:8887`). |
| **connect-discovery-uri** | | `""` | Optional full router-info WebSocket URI (e.g. `wss://host:4443/router-info`). When set, used for discovery. Note: ThingPark Community may not listen on 4443 (connection can timeout). |
| **connect-discovery-path** | | `/router-info` | Router-info path on the same host/port as **connect-uri**. Ignored if **connect-discovery-uri** is set. |
| **connect-data-path** | | `""` | Path for the LNS data WebSocket on the same host/port as **connect-uri**. Required when the upstream serves a web UI at the root (e.g. ThingPark: set to the LNS WebSocket path such as `/tpe/lns`). When empty, data URI is **connect-uri** as-is. |
| **connect-data-uri** | | `""` | Full LNS data WebSocket URI. When set, overrides connect-uri + connect-data-path for data; also used as the host for router-info discovery (connect-discovery-path) when **connect-discovery-uri** is not set, so discovery is tried on the correct host (e.g. lns.thingpark.com) instead of connect-uri (e.g. community.thingpark.io). |
| **gateway** | yes** | `""` | Gateway ID in the analytics platform (unique per gateway). |
| **listen-host** | | `0.0.0.0` | Bind address for the WebSocket server. |
| **listen-port** | | `8090` | Port for the WebSocket server. |
| **listen-tls-cert** | | `""` | TLS certificate file for incoming (gateway) connections. |
| **listen-tls-key** | | `""` | TLS key file for incoming connections. |
| **certs-dir** | | `./certs` | Directory for CA, bootstrap and per-gateway certs; when set, enables per-gateway CUPS client certs and optional bootstrap listen cert. |
| **certs-ca-cert** | | `""` | Path to existing CA certificate (default: generate in certs-dir). |
| **certs-ca-key** | | `""` | Path to existing CA key (use with **certs-ca-cert**). |
| **certs-upstream-client-cert** | | `false` | If `true`, use proxy-issued client cert for **CUPS** when no server-issued cert is stored. Only set `true` when upstream LNS/CUPS trusts the proxy CA (e.g. your own LNS). For third-party (ThingPark, TTN) keep `false`. WebSocket (router-info/data) always uses a client cert when available. |
| **connect-tls-ca** | | `""` | CA certificate file to verify the upstream LNS. |
| **connect-tls-insecure** | | `false` | Skip TLS verification for the upstream LNS. |
| **analytics-endpoint** | | `ingress.eu1.cluster.kudzu.gr:443` | Analytics gRPC endpoint. |
| **analytics-ca-file** | | `""` | Optional CA PEM appended to the system trust store for analytics gRPC and pairing HTTPS. |
| **analytics-connect-timeout** | | `0` | Connection timeout (seconds) for analytics. |
| **analytics-request-timeout** | | `0` | Request timeout for analytics push. |
| **analytics-max-backoff** | | `0` | Max reconnection backoff (seconds). |
| **flush-interval** | | `5`/`10`*** | Seconds between metric flushes. |
| **server-side** | | `false` | Run in server-side mode (multiple gateways). |
| **gateway** | | `""` | Gateway ID (required when **server-side** is false). |
| **max-ws-streams** | | `2`/`256`*** | Max concurrent gateway WebSocket sessions. |
| **gauge-stat** | | `false` | Treat stats as gauges. |
| **config** | | `""` | Path to config file. |
| **log-level** | | `info` | Log level: `error`, `warn`, `info`, `debug`. |
| **verbose** | | `false` | Enable debug log level and hexdump of incoming/outgoing traffic (HTTP and WebSocket). |
| **log-file** | | `""` | Redirect logs to a file. |
| **debug-dump** | | `""` | Dump all WebSocket traffic to a file (debug). |
| **version** | | | Print version and exit. |
| **pairing-endpoint** | | `https://console.eu1.cluster.kudzu.gr` | HTTPS base URL used only for pairing. |
| **pair-pin** | | | Fetch config from Kudzu using a pairing PIN and exit. |
| **write** | | | With **pair-pin**, write fetched config to the file given by **config**. |

\* Omitted only when using **pair-pin** to fetch config.  
\** Omitted when **server-side** is true.  
\*** Default depends on **server-side** (5 / 10 for flush-interval, 256 / 2 for max-ws-streams).

### Environment variables

Any option can be set via environment variables: use the option name in UPPER_SNAKE_CASE (e.g. `CONNECT_URI`, `CLIENT_ID`).

### Pairing (PIN-based config)

To generate a config from the Kudzu Analytics pairing PIN (uses the shared `client` pairing API with system roots, plus `--analytics-ca-file` when set):

```sh
# Print config to stdout
/usr/bin/kudzu-forwarder-bs --pair-pin=123456

# Fetch and write to config file
/usr/bin/kudzu-forwarder-bs --pair-pin=123456 --config=/etc/kudzu-forwarder-bs.conf --write
```

Default pairing endpoint: `https://console.eu1.cluster.kudzu.gr`. The client
appends `/api/v1/pairing/edge`. Override the base URL with
`--pairing-endpoint`, the `pairing-endpoint` config key, or
`PAIRING_ENDPOINT`; `analytics-endpoint` remains the independent gRPC ingress.

## Build instructions

Requirements:

- [Go](https://go.dev/doc/install) 1.18 or newer

Build from the repository root:

```sh
git clone https://github.com/kudzutechnologies/analytics.git
cd analytics

# Build for current OS/arch
go build -o kudzu-forwarder-bs ./forwarder-bs/

# Cross-compile (examples)
GOOS=linux GOARCH=amd64 go build -o kudzu-forwarder-bs-amd64 ./forwarder-bs/
GOOS=linux GOARCH=arm GOARM=7 go build -o kudzu-forwarder-bs-arm7 ./forwarder-bs/
```

Or use the Makefile inside `forwarder-bs` (run from that directory; the parent `go.mod` is used):

```sh
cd forwarder-bs
make kudzu-forwarder-bs-amd64
```

## Version

```sh
/usr/bin/kudzu-forwarder-bs --version
```

Output format: `Kudzu Analytics Basic Station Forwarder vX.Y.Z (Git <revision>)`.
