# Analytics downlink delivery review

Acceptance status: satisfied against AD-01–04.

The user explicitly authorized implementing the Analytics feature, creating this
branch and opening a PR using repository-local git authorship. The acceptance
record is `docs/changes/downlink-delivery.yaml`, SHA-256 `5ed2492da026145f90b5bdcd83b1c9e87656f3ea4e0f140c45feb66e1dce4b0f`.
Baseline: `8b7ccd7af84f419e8555340687f4fc5594e2ab42`.
Branch: `feat/downlink-delivery-tracking`. Effective local git author:
`Kudzu Tech <info@kudzu.gr>`. Existing untracked Basic Station, certificate/build
and tools directories are untouched and excluded from this review.

## Behavior and diff map

UDP requests previously had packet telemetry but no per-command outcome. The
additive API now carries a gateway/token identity plus raw-command fingerprint,
and separate TX_ACK observations with explicit acceptance, native rejection or
unknown evidence. Older servers ignore the outcome array without fake packet
history; old clients remain supported by the companion engine.

- `api/analytics.proto` and generated Go/C++ bindings: two additive message fields,
  two new message types and one enum; gRPC method signatures are unchanged.
- `forwarder` and `forwarder-cc`: equivalent source observations, full LoRa/FSK
  metadata, empty-ACK semantics, strict unknown handling, endpoint/envelope
  ownership, copied Go input bytes, synchronized flush/eviction and outcome caps.
- `api/testdata`: shared Go/C++/engine binary fixture and raw command test vector.
- `docs/downlink-delivery.md` and package guides: contract, rollout and boundaries.

## Acceptance evidence

| Criterion | Observed evidence |
| --- | --- |
| AD-01 | `api/TestDeliveryWireCompatibility` and existing schema compatibility tests passed. One packet plus one outcome decodes as only one packet on an old receiver. Unknown outcome bytes survive an old relay. |
| AD-02 | Go observation tests passed explicit/empty success, rejection, malformed/missing/null error, replay fingerprints, caller-buffer mutation, configured identity, NAT peer/EUI changes, ACK-only snapshots, concurrent snapshots, capacity flush and numeric FSK. Native `delivery_test` checks corresponding C++ behavior and shared fixtures. |
| AD-03 | Engine ingress passed 19 tests/subtests with the published Analytics SDK and 19 with the local new SDK. New tests validate legacy compatibility, command/ACK conversion, complete candidate metadata, raw GPS/concentrator evidence, FSK deviation, malformed metadata and stream-emission errors. |
| AD-04 | `TestSemtechDeliveryRecovery` passed early ACK, timeout/late rejection/success, replay, ambiguous reuse, interrupted projection and eight concurrent restart claims, with exactly five commands/activity effects in both mirrors. The real two-shard fixture separately passed all seven tests/subtests, including Semtech recovery and existing sharded routing/transaction/type-completion checks. |

Analytics validation from this module root:

```sh
GOWORK=off GOFLAGS='-buildvcs=false' go test -json -count=1 ./...
GOWORK=off GOFLAGS='-buildvcs=false' go test -json -race -count=1 -run 'TestDownlinkDelivery|TestDownlinkConfiguredIdentity' ./forwarder
GOWORK=off GOFLAGS='-buildvcs=false' go vet ./...
cmake -S forwarder-cc -B /tmp/kudzu-analytics-delivery-cpp -DFORWARDER_CC_BUILD_TESTS=ON -DCLIENT_CC_BUILD_TESTS=ON -DCMAKE_OSX_SYSROOT="$(xcrun --show-sdk-path)"
cmake --build /tmp/kudzu-analytics-delivery-cpp -j 4
ctest --test-dir /tmp/kudzu-analytics-delivery-cpp --output-on-failure
```

Results: 56 Go tests/subtests and 15 focused race tests/subtests passed with no
skips. All 11 native tests passed. Scoped vet and whitespace checks passed.
Repeating `make api` reproduced all 11 generated binding files byte for byte.
The C++ bindings were regenerated together with protoc/runtime 36.2/7.36.2 and
gRPC C++ 1.85.2; Go module dependencies and runtime configuration are unchanged.

Engine validation additionally ran the affected ingress, Mongo, OTAA and provider
regressions: 285 passes and eight opt-in fixture/load skips. Mongo's focused race
run passed 19 tests/subtests with one opt-in sharded skip; ingress race passed 19.
Docker sharded recovery separately ran without skips. The canonical Tools Context
drift check passed; Tools and Deployment Context prose and the data-model rule
were reconciled. No native Orlet tool or role boundary changed.

## Code economy and review focus

This reuses the engine's durable token/mailbox mechanism rather than adding a
forwarder correlation cache, persistence layer, RPC method or dependency. New
concepts are command identity and independent ACK evidence, with the existing
batch owner extended to cover atomic snapshots and immutable gateway envelopes.
A C++ test snapshot hook follows the existing inspection hooks. Review endpoint
identity changes, the empty-versus-malformed ACK distinction, and old-server
unknown-field handling most closely. The read-only diff reporter was inspected;
most diff volume is regenerated protobuf C++ output, including unchanged RPC
messages rebuilt for the matching runtime version.

## Departures and limits

The first native fixture test incorrectly required byte-identical serialization
across languages. Protobuf field order is not canonical across Go/C++ (notably
oneofs); the corrected oracle checks complete message equality after round trip
and exact cross-language command fingerprints. Acceptance remains semantic wire
compatibility. One earlier Go proxy test hit its existing 100 ms UDP timeout
while native compilation ran; the unchanged full suite subsequently passed.
Stale native build caches referenced removed SDK/gRPC versions; clean builds with
the installed SDK/runtime now pass. The final native gate also caught ambiguous
assertion overloads in the new FSK test; those assertions now use exact boolean
comparisons. Intermediate CTest output from an older binary after that failed
build was excluded. The final build and test commands are gated sequentially,
and both must succeed. No assertions were relaxed to hide a source
failure and no required tests were skipped.

The companion engine translator/reconciliation changes are prepared locally in
`kudzu-engine` and are not part of this Analytics commit or pushed by this request.
Deploy that translator first. UDP/gRPC buffers, reconnect policy and earlier Core
NATS remain best effort; recovery starts at majority-acknowledged Mongo admission.
The native forwarding path has no new throughput or real-firmware certification.
Gateway acceptance proves neither emission nor device receipt. Midstream requests
without known/configured gateway identity may remain safely unmatched; ambiguous
reused tokens never select a command. Basic Station/untracked adapters and shared
production rollout are outside this unit.
