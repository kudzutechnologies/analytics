# Kudzu Basic Station library

Package `forwarderbs` embeds a Basic Station CUPS/LNS proxy and analytics
collection in Go applications. It forwards discovery and data WebSockets while
collecting analytics in a bounded memory queue. Analytics outages do not block
radio forwarding.

For the standalone executable, configuration files, gateway registration, and
command-line bootstrap export, see the [CLI README](cli/README.md).

## Quick start

Import `github.com/kudzutechnologies/analytics/forwarder-bs` as `forwarderbs`.
Start with the default configuration rather than a zero-value struct:

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"

    forwarderbs "github.com/kudzutechnologies/analytics/forwarder-bs"
)

func main() {
    config := forwarderbs.DefaultForwarderConfig()
    config.ConnectURI = "wss://lns.example.test"
    config.ClientId = os.Getenv("CLIENT_ID")
    config.ClientKey = os.Getenv("CLIENT_KEY")
    config.GatewayId = os.Getenv("GATEWAY")

    forwarder, err := forwarderbs.New(config, forwarderbs.Dependencies{})
    if err != nil {
        log.Fatal(err)
    }
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()
    if err := forwarder.Run(ctx); err != nil {
        log.Fatal(err)
    }
}
```

This local-mode example binds to loopback. The gateway EUI used by Basic Station
is distinct from the analytics platform's gateway ObjectID (`GatewayId`). Supply
`GatewayEUI` when a direct connection needs an identity without discovery.

## Using an existing HTTP server

Call `Start(ctx)` to start analytics and lifecycle monitoring without opening a
listener. Mount `Handler()` on your server and use `TLSConfig()` when terminating
TLS there. On shutdown, stop the host server and call `Shutdown(ctx)` with a
bounded context; WebSocket connections require the forwarder's own shutdown.
See the [embedding example](example_test.go).

Shared mode requires TLS, `PublicURI`, and gateway authorization. Preserve
`tls.RequestClientCert` in the supplied TLS configuration so the handler receives
peer certificates. Certificate pins and/or ingress tokens authorize the gateway;
a certificate CN alone grants no access. The handler expects actual TLS request
state; forwarded headers do not replace it. Mount at the public root path.
`TLSConfig()` returns nil when listener certificate loading fails or no listener
certificate is configured.

## API reference

Signatures below describe the primary embedding API. Run
`go doc ./forwarder-bs` from the repository root for the complete exported API.

### Construction and lifecycle

| API | Behavior |
|---|---|
| `DefaultForwarderConfig() ForwarderConfig` | Returns default resource, timeout, endpoint, and proxy settings. |
| `New(ForwarderConfig, Dependencies) (*Forwarder, error)` | Validates configuration and constructs the proxy and analytics collector without starting workers or a listener. May read configured files and open an explicitly configured debug dump. |
| `(*Forwarder).Run(context.Context) error` | Starts workers and an owned HTTP/TLS listener; cancellation or a server error triggers shutdown. |
| `(*Forwarder).Start(context.Context) error` | Starts workers and context monitoring for a host-owned HTTP server. The first successful call supplies the lifetime context. |
| `(*Forwarder).Handler() http.Handler` | Returns the discovery, data, and managed CUPS handler. |
| `(*Forwarder).TLSConfig() *tls.Config` | Returns listener TLS settings, including client certificate requests. |
| `(*Forwarder).Shutdown(context.Context) error` | Idempotently closes proxy sessions and drains analytics within the supplied deadline. Does not stop a host-owned server. |
| `(*Forwarder).DeliveryStats() DeliveryStats` | Returns queue retention, drop, and analytics failure counters. |

Instances cannot restart after shutdown. Queue retention is memory-only: process
exit or a shutdown deadline can lose pending telemetry. Ambiguous RPC results can
produce duplicates. These counters do not establish that radio traffic reached
the LNS.

### Configuration and dependencies

`ForwarderConfig` retains the same settings used by the CLI. Its transport fields
control discovery/data URIs, allowed origins, TLS, managed CUPS, and local/shared
mode; its resource fields bound queue sizes and active sessions. See
[config.go](config.go) for every field and the [CLI configuration reference](cli/README.md#configuration-and-retention)
for defaults and file/environment mappings.

Embedded callers can supply material directly:

| Fields | Purpose |
|---|---|
| `CertsCACertPEM`, `CertsCAKeyPEM` | Trust CA certificate and optional signing key. |
| `IssuerCertPEM`, `IssuerKeyPEM` | Optional separate issuer signing pair. |
| `ListenTLSCertPEM`, `ListenTLSKeyPEM` | Listener certificate chain and private key. |
| `ConnectTLSCAPEM`, `ConnectDataTLSCAPEM` | Additional upstream trust, including a data-specific override. |
| `GatewayAuthorizations` | Gateway authorization records without a manifest file. |
| `Logger` | Instance logging through `logrus.FieldLogger`. |

PEM inputs take precedence over corresponding file paths. They and the inline
logger/authorization fields are excluded from config serialization and automatic
flag generation. No CA is generated implicitly.

`Dependencies` accepts:

- `Persistence GatewayPersistence`: custom storage; otherwise `CertsDir` selects
  `FilesystemPersistence` when configured.
- `Analytics AnalyticsTransport`: custom analytics transport; otherwise a client
  is created from the analytics fields in `ForwarderConfig`.
- `Logger logrus.FieldLogger`: overrides the configuration logger for this instance.

`GatewayAuthorization` contains `Router`, certificate `Pins`, an `IngressToken`,
and purpose-specific `TCAuth` / `CUPSAuth` headers. Inline token/header values must
include the intended authorization scheme, such as `Bearer `. Pins and ingress
tokens must uniquely identify gateways.

The reusable parsing helpers do not execute CLI actions or exit the process:

```go
func ParseForwarderConfig(args, environ []string) (ForwarderConfig, error)
func ParseConfigFromEnv() (ForwarderConfig, error)
func ValidateForwarderConfig(*ForwarderConfig) error
```

`ParseConfigFromEnv` reads process arguments and environment variables. Parsing
uses defaults → config file → environment → arguments; help returns `flag.ErrHelp`.
Validation resolves the mode-dependent stream limit in the supplied config.
`RenderPairConfig(pin, config)` fetches and renders pairing configuration;
`Version()` reports the build's VCS revision or `"unknown"`.

### Persistence

```go
type GatewayPersistence interface {
    Load(context.Context, string) (GatewayRecord, error)
    Save(context.Context, GatewayRecord, uint64) (uint64, error)
}

type GatewayPatcher interface {
    Patch(context.Context, string, GatewayRecordPatch, uint64) (uint64, error)
}
```

`GatewayRecord` contains the canonical EUI and revision, gateway client PEM pair,
upstream CUPS/TC credential blobs, CUPS endpoint state, and saved LNS configuration. `CertificatePEM`, `CUPSState`, and
`GatewayLNSConfig` represent those nested values. Records and patches have JSON
and BSON tags and require no database driver.

`Save` replaces a complete record and checks the expected revision atomically.
Revision zero supports initial creation. Successful changed
writes return the new revision. `GatewayPatcher` is optional: the library falls
back to load/merge/save when it is absent. Conflicts are retried up to three times.

Patch nil fields mean unchanged; supplied empty values clear data. Nested groups
replace the complete supplied group. Supply an empty `GatewayLNSConfig` to clear
that group. Backends must preserve unrelated fields and enforce revisions under
concurrent access. Treat the records as private credential material.

| Error | Meaning |
|---|---|
| `ErrNotFound` | No gateway record exists. |
| `ErrReadOnly` | The backend rejects writes. |
| `ErrConflict` | The expected revision does not match. |

Use `errors.Is` when checking these errors. Other storage failures propagate;
they are not permission to replace existing credentials.

`FilesystemPersistence{Dir: path, ReadOnly: false}` implements both interfaces.
It writes private `<eui>.gateway.json` files using atomic replacement and skips
unchanged writes. Missing records return `ErrNotFound`; malformed records return
an error. Use one owning process per store directory.

### Gateway bootstrap certificates

```go
func IssueGatewayBootstrap(ForwarderConfig, string) (GatewayBootstrap, error)
func ExportGatewayBootstrap(ForwarderConfig) error
```

`IssueGatewayBootstrap` returns material without writing files. `GatewayBootstrap`
contains canonical `EUI`, CA PEM `Trust`, client certificate chain `Cert`, private
key PEM `Key`, and the leaf certificate SHA-256 `Pin`.

The issuer defaults to the CA signing pair. An explicit issuer certificate/key
must chain to the configured trust CA and permits the root private key to remain
offline. Certificates use P-256 keys, the EUI as CN, client-auth usage, and up to
one year of validity capped by issuer expiry. `ErrIssuanceDisabled` means signing
material is unavailable. Existing runtime credentials remain usable without it.

`ExportGatewayBootstrap` uses `BootstrapGW` and `BootstrapDir`, defaulting the root
to `./bootstrap`. It writes `cups.trust`, `cups.crt`, and `cups.key` beneath the
canonical EUI directory and prints its location and pin. It refuses an existing
output directory and does not change authorization or runtime persistence.
The trust file copies the supplied CA PEM. See the [CLI bootstrap guide](cli/README.md#export-gateway-bootstrap-credentials)
for the command-line equivalent.

### Lower-level APIs

For applications assembling their own components,
`NewCertStore(GatewayPersistence, ForwarderConfig)` constructs record-backed
credential access with optional signing. `NewWSProxy(*WSProxyConfig)`
constructs the proxy, `WSProxyEvents` receives forwarded messages for analytics,
and `CreateAnalyticsForwarder` constructs the collector. These callers must wire
events and manage startup/shutdown themselves; `New` handles that integration.
Identity helpers include `ParseGatewayID` (validated canonical EUI) and
`NormalizeGatewayID` (empty string on invalid input). Protocol parsers and CUPS
encoding helpers are also exported; inspect them with `go doc`.

## Verification

From the repository root:

```sh
go test ./client ./forwarder-bs/...
go test -race ./client ./forwarder-bs/...
go vet ./forwarder-bs/...
```

Executable build instructions and release packaging are in the
[CLI README](cli/README.md#build-and-release).
