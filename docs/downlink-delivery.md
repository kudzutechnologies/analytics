# Semtech UDP downlink delivery evidence

Go and C++ forwarders publish each observed PULL_RESP as the existing complete
`AnalyticsDownlink`, now with optional `delivery` metadata (field 22). Its `id`
combines the configured gateway ID when supplied, otherwise the observed EUI,
otherwise the UDP peer endpoint, with the reusable 16-bit token. `commandKey` is
lowercase SHA-256 of the raw PULL_RESP body. It is a command fingerprint, not a
MIC, authentication claim, uplink association token, or the legacy packet checksum.
Different command bodies sharing a token stay distinguishable; exact datagram
replays retain their identity. Across gateways, identical packet bytes establish
no shared logical downlink.

TX_ACKs are separate `AnalyticsMetrics.downlinkOutcomes` entries (field 8), never
synthetic packet entries. They contain the same scoped token identity, explicit
status, native rejection reason and the forwarder's observation time in Unix
milliseconds. Per the [Semtech UDP protocol](https://github.com/Lora-net/packet_forwarder/blob/master/PROTOCOL.TXT#L296),
an empty TX_ACK body or explicit `NONE` is acceptance. Other explicit nonempty
errors mean rejection, retaining their native code. Malformed JSON, missing/null/
non-string/empty error values and overlong reasons remain unknown. Truncated
EUI-bearing packets are ignored. Gateway acceptance is not proof of emission
or device receipt. Aggregate TX_ACK and gateway TX counters remain separate.

Batches are scoped to the complete peer endpoint rather than IP alone. Mutation,
flush snapshots and LRU eviction share ownership locks; gRPC writes happen after
releasing the batch lock. A changed EUI on one endpoint flushes its original
envelope before new evidence is queued. Go copies input-buffer gateway bytes.
ACK-only batches are flushed and their outcome arrays reset with other data.
A full 4,096-entry outcome batch is flushed before accepting another entry,
matching the engine receiver limit without introducing a new configuration.

## Engine coordination and compatibility

The engine's `service-ingress-api` must translate the additive fields into
`semtech_udp` delivery observations and candidate snapshots. The companion engine
change reads both the currently published Analytics schema's unknown fields and
the new generated schema, without depending on an unpublished module version.
It preserves raw concentrator/GPS timing evidence for the existing gateway-scoped
estimator rather than treating collection time as a transmit schedule.

Mongo admission and reconciliation are owned by the engine, reusing its existing
durable routing/ACK mailbox. ACKs may arrive before the command or after timeout;
explicit later evidence can refine unknown. Repeated commands/outcomes do not
repeat activity effects. Reused tokens with multiple eligible commands stay
ambiguous; a bare ACK selects neither. Missing ACKs become unknown. No new
forwarder correlation cache or Redis dependency is introduced.

Deploy the matching engine first, then upgrade forwarders independently. Older
servers ignore the new metadata and outcome array while retaining normal packet
and aggregate handling. New servers continue accepting legacy clients without
fabricating ACK evidence. The Go/C++ binary fixture under `api/testdata` is also
consumed by the engine's ingress compatibility test.

This does not add durable UDP or gRPC delivery. Forwarder buffers and earlier Core
NATS transport remain best effort; the engine's recovery guarantee begins at
majority-acknowledged Mongo admission. Observing a request midstream without prior
gateway identity may leave peer-scoped evidence that cannot be safely associated
with a later EUI-scoped ACK. Configure the gateway ID or observe PULL_DATA first.
The two-shard engine recovery checks do not certify real gateway firmware,
provider access, multi-node failover or forwarder throughput. Basic Station and
untracked adapters are outside this change.

## Development validation

Use matching protoc/C++ runtime versions when regenerating bindings. This revision
uses protoc 36.2, protoc-gen-go 1.33.0, protoc-gen-go-grpc 1.3.0, protobuf C++ 7.36.2
and gRPC C++ 1.85.2. Go module requirements remain unchanged.

```sh
make api
GOWORK=off GOFLAGS='-buildvcs=false' go test -json -count=1 ./...
GOWORK=off GOFLAGS='-buildvcs=false' go test -race -count=1 -run 'TestDownlinkDelivery|TestDownlinkConfiguredIdentity' ./forwarder
GOWORK=off GOFLAGS='-buildvcs=false' go vet ./...
cmake -S forwarder-cc -B /tmp/analytics-delivery-build -DFORWARDER_CC_BUILD_TESTS=ON -DCLIENT_CC_BUILD_TESTS=ON
cmake --build /tmp/analytics-delivery-build -j 4
ctest --test-dir /tmp/analytics-delivery-build --output-on-failure
```

On macOS a clean build can additionally select the installed SDK using
`-DCMAKE_OSX_SYSROOT="$(xcrun --show-sdk-path)"`; stale build caches may reference
removed SDKs or gRPC libraries. Regenerating with an incompatible C++ runtime is
not a supported workaround.
