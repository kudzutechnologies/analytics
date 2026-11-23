# Kudzu Basic Station proxy

`kudzu-forwarder-bs` forwards Basic Station discovery and LNS WebSockets and
collects analytics in a bounded memory queue. An unavailable analytics backend
does not block radio forwarding. This proxy terminates TLS and needs its own
pre-provisioned listener identity and upstream access.

## Local gateway

```ini
# /etc/kudzu-forwarder-bs.conf (0600)
proxy-mode=local
listen-host=127.0.0.1
listen-port=8090
connect-uri=wss://lns.example.test
client-id=YOUR_ANALYTICS_CLIENT_ID_HEX
client-key=YOUR_ANALYTICS_CLIENT_KEY_HEX
gateway=YOUR_KUDZU_GATEWAY_OBJECT_ID
```

Run `kudzu-forwarder-bs --config=/etc/kudzu-forwarder-bs.conf`. Configure Station
`tc.uri` to `ws://127.0.0.1:8090`; keep CUPS pointed at the original CUPS service
unless managed CUPS has been deliberately configured. Local mode accepts only
loopback bindings. Set `gateway-eui` when a direct connection needs a router
identity without discovery. EUI/ID6 identity is distinct from the analytics
platform's gateway ObjectID.

## Shared proxy

Shared mode requires explicit TLS, a public root WebSocket URI, and a registry
of already authorized gateways. Unknown gateways cannot enroll themselves.

```ini
proxy-mode=shared
listen-host=0.0.0.0
listen-tls-cert=/etc/kudzu/listener.crt
listen-tls-key=/etc/kudzu/listener.key
public-uri=wss://proxy.example.test:8090
gateway-auth-file=/etc/kudzu/gateways.json
server-side=true
```

Supply the same analytics credentials and upstream settings as in local mode.
`server-side` controls analytics platform identity handling; `proxy-mode`
controls ingress authorization. The registry is loaded at startup; restart to
apply changes. It accepts at most 4096 gateways and 1 MiB of JSON:

```json
{
  "version": 1,
  "gateways": [{
    "router": "0102030405060708",
    "client_cert_sha256": ["REPLACE_WITH_64_HEX_DIGIT_SHA256_OF_CLIENT_CERT_DER"],
    "ingress_token_file": "gateway-a.ingress-token",
    "upstream_tc_token_file": "gateway-a.tc-token",
    "upstream_cups_token_file": "gateway-a.cups-token"
  }]
}
```

Use a client certificate pin, an ingress token, or both. Pins and ingress tokens
must uniquely identify a gateway. Certificate validity and client-auth usage
are checked. If both proofs are supplied they must identify the same gateway.
A certificate common name alone grants no access. Token paths are relative to
the registry; token files must be regular and private (`0600`, at most 8192
bytes). A token without a scheme gets `Bearer ` prepended. Ingress proof is
separate from upstream TC and CUPS credentials. Authorization happens before
upstream credential selection. The router claimed in a query/discovery body
must match the authenticated identity.

## LNS routing and TLS

`connect-uri` is required. Discovery uses `connect-discovery-uri` when set,
otherwise the base/data host plus `connect-discovery-path` (default
`/router-info`). Data uses the URI assigned by discovery, or the explicit
`connect-data-uri` / `connect-data-path` override. Discovery HTTP 404 enables a
synthetic response using the configured data endpoint. Other failures remain
failures. Discovery responses advertise `public-uri`; its path must be root.
Request Host headers do not determine the public destination.
Advertised data URIs include an explicit root slash before the router query;
Station rejects a discovery URI with no path. This normalization does not alter
the CUPS URI or require adding a trailing slash to `public-uri`.

The discovery response's `router` is validated as the gateway identity. String
`muxs` endpoint labels are forwarded unchanged; they are not authorization or
routing decisions. Explicit upstream rejections remain rejections even if their
identity fields are empty. Decode failures report the failing field without
dumping the upstream response body.

An assigned destination or redirect must match a configured upstream origin.
Add other legitimate destinations with comma-separated
`upstream-allowed-origins`. Origins compare scheme, hostname, and effective
port; HTTPS and WSS share an origin. `connect-ws-subprotocol` selects an explicit
subprotocol. The Station version message is forwarded unchanged unless
`inject-router-into-version=true` is set. Both directions receive heartbeats;
replaced/closed sessions release identity-specific runtime state.
The 60-second read inactivity deadline refreshes on incoming application
messages, ping frames, and pong frames. A silent peer still times out; active
traffic is not discarded merely because it omits pong replies to proxy pings.

System roots verify upstream TLS by default. `connect-tls-ca` adds a CA;
`connect-data-tls-ca` and `connect-data-tls-server-name` customize the data
connection. `connect-tls-insecure` and `connect-data-tls-insecure` explicitly
disable verification and are unsuitable for normal release configurations.
Listener TLS requires both `listen-tls-cert` and `listen-tls-key`. Normal startup
does not generate a listener certificate or a CA.

An optional `certs-dir` imports existing upstream credentials. Canonical router
filenames are `{router}.server.crt` / `.server.key` for CUPS and
`{router}_tc.server.crt` / `.server.key` for TC. Purpose-specific credentials
never fall back to the other purpose. Provision the matching trust/key files
privately. Proxy-issued client certificates are used only with explicit
`certs-upstream-client-cert=true`, when the upstream trusts that CA.

## Managed CUPS

Default `cups-mode=off` leaves CUPS outside the proxy. `cups-mode=managed`
requires `certs-dir`, an explicit listener certificate signed by the configured
proxy CA, and a valid public TLS hostname. Provision ingress authorization and
upstream access before starting. Station must trust the proxy listener for
both TC and CUPS. This mode intentionally issues proxy credentials and writes
changed upstream state; it does not enable unauthenticated first enrollment.

In this mode `connect-uri` identifies the initial upstream CUPS service. Until
CUPS supplies a TC URI, the proxy reports an empty upstream `tcUri` (or the
explicit `connect-data-uri` override). It never substitutes the CUPS host as an
LNS endpoint. LNS discovery returns a provisioning-pending error and direct
data connections return HTTP 503 until an endpoint is assigned or explicitly
configured. A discovery HTTP 404 falls back only to a known LNS data endpoint.

Unchanged updates return the 14-byte zero response. Credentials are compared
with Basic Station CRC semantics; unchanged files are not rewritten. Changed
binary credentials are stored atomically as purpose-specific `.server.cred`
files. `cups-max-credential-bytes` defaults to 16384 (range 1024–65535).
Malformed DER, lengths, and signatures are rejected. Firmware responses require
a known Content-Length and stream with a small fixed buffer; there is no
firmware spool. Owners must manage certificate renewal and provisioning.

## Configuration and retention

Precedence is defaults → INI → environment → CLI, including explicit default
values. Options use kebab case; environment variables use UPPER_SNAKE_CASE.
`--config` / `CONFIG` select a file. Unknown options, conflicting aliases,
invalid integers, and invalid profiles fail startup.

| Control | Default | Minimum / meaning |
|---|---:|---|
| `max-batches` | 2 | 1; global, including partial and in-flight batches |
| `max-messages` | 32 | 1 per batch; statistics count as messages |
| `max-buffer-bytes` | 65536 | 1024; charged serialized telemetry, not process RSS |
| `batch-sec` | 10 | 1 second; `flush-interval` / `FLUSH_INTERVAL` alias |
| `max-ws-streams` | 1 local / 256 shared | 1; includes pending connections |
| `shutdown-deadline-ms` | 5000 | 0; one deadline for listener, sessions, and sender |
| `analytics-connect-timeout` | 5 | 1 second |
| `analytics-request-timeout` | 5 | 1 second |
| `analytics-max-backoff` | 60 | 1 second; exponential retry cap |

Source [minimum.env](minimum.env) for the smallest supported retention profile;
supply credentials and endpoints separately. A pinned in-flight batch consumes
capacity. When full, oldest waiting telemetry is evicted; new telemetry is
rejected if only in-flight work remains. Acknowledged batches leave the queue;
failed pushes remain for retry. Permanent INVALID_ARGUMENT responses are
counted and discarded. Statistics refused at a flush/session close are counted
as loss. Restart and deadline expiry can lose retained telemetry. RPC ambiguity
can produce duplicate delivery. There is no disk spool or exactly-once promise.
`pre-identity-max-messages` is accepted for profile compatibility; this proxy
collects only after a router has been resolved and authorized.

`gauge-stat` defaults false. `analytics-endpoint` defaults to
`ingress.eu1.cluster.kudzu.gr:443`; `analytics-ca-file` appends a CA for analytics
and pairing. `--pair-pin=PIN` fetches a configuration; add `--write --config=PATH`
to save it privately. `pairing-endpoint` defaults to
`https://console.eu1.cluster.kudzu.gr` and appends `/api/v1/pairing/edge`.

Logs default to stderr at `info`; no application log file is created.
Managed CUPS logs the upstream HTTP status, response length, URI origins, and
credential/update byte counts. An empty upstream response with zero local
credential CRCs produces a warning: issuing proxy credentials does not establish
that the gateway obtained upstream credentials or connected to the LNS. Discovery
and data connection logs show connection stages and HTTP/TLS failure categories.
Data sessions also log forwarding of `version` and `router_config`. Minute and
close summaries include session age, frame counts, handshake flags, and ping/pong
counts for each peer. Read failures identify the side and distinguish timeouts
from WebSocket close codes. A WebSocket connection alone does not establish that
Station has accepted the radio configuration or that radio traffic is flowing.
These diagnostics omit credential bodies, request/response payloads, URL userinfo,
paths, and query strings.
`log-level` accepts error/warn/info/debug. Minute and shutdown summaries report
retention, loss reasons, and connection/push failures without payloads or keys.
`log-file` is an explicit operator-managed file. `verbose` enables packet debug
logging; `debug-dump=PATH` writes raw WebSocket records privately, capped by
`debug-dump-max-bytes` (default 1048576, zero disables). Existing file bytes
consume that cap. Debug traffic can contain sensitive payloads; keep diagnostics
brief and remove them after use. Runtime memory, protobuf allocations, TLS,
sockets, and per-session state add to the telemetry byte budget.

## Build and release

Use Go 1.20+ for the repository's dependency and test graph:

```sh
go build -trimpath -ldflags='-s -w' -o kudzu-forwarder-bs ./forwarder-bs
go test ./client ./forwarder-bs
go test -race ./client ./forwarder-bs
python3 tools/build-go-forwarders.py --component bs --version dev --output-dir /tmp/bs-artifacts
```

The script builds all six declared Linux targets (amd64, ARM5/6/7 and both MIPS
softfloat endiannesses), preserving bounds checks and producing checksummed
archives. `--version` reports the injected release version and source revision.
See [release verification](../docs/basicstation-release.md) for migration,
external gates, measurements, and the current release status.
