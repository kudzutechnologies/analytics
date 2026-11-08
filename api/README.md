# api

> Protobuf messages and gRPC interfaces for gateway analytics ingestion.

## Use this package for

- Sending gateway reception, transmission and statistics batches.
- Keeping old and new gateway clients compatible during independent upgrades.

## Key abstractions

- `AnalyticsMetrics` — a gateway-scoped batch.
- `AnalyticsUplink` — reception metadata, a legacy header and optional complete encrypted frame.
- `AnalyticsDownlink` — transmission metadata and the corresponding frame bytes.

## Example

Send a complete encrypted downlink while retaining the header for older servers.

```go
frame, _ := hex.DecodeString("60da1b012620010001102030a8d99bc7")
batch := &api.AnalyticsMetrics{
    GatewayId: "gateway-1",
    Downlinks: []*api.AnalyticsDownlink{{
        Size: uint32(len(frame)), Fhdr: frame[:9], PhyPayload: frame,
    }},
}
_ = batch
```

`PhyPayload` includes the on-air MIC; it does not contain decrypted application data.

## Boundaries

- Keep `Fhdr` populated for legacy receivers. `PhyPayload` and `GatewayContext` are optional additive fields.
- Preserve gateway context only when the source supplies it; packet checksums are not uplink/downlink relationship tokens.
- Older receivers ignore the added fields; newer receivers continue accepting legacy header-only messages.
