# Temporary guide: Basic Station → wavesoft.gr proxy → ThingPark

Use managed CUPS, with the hosted server at `wavesoft.gr` serving both
provisioning and LNS traffic:

```text
Gateway ──HTTPS/WSS──▶ wavesoft.gr:8090 ──HTTPS/WSS──▶ ThingPark
```

For onboarding, select **CUPS** on the gateway. It receives the proxy's LNS
address automatically. Selecting only LNS skips certificate provisioning.

This example uses ThingPark Community and a Linux server you can access
over SSH. The gateway must resolve and reach `wavesoft.gr` on TCP/8090. The
configuration is based on the Basic Station branch implementation; live
compatibility with the particular gateway firmware and ThingPark tenant still
needs verification.

## 1. Prepare the hosted server and generate certificates

Connect to the server, replacing `YOUR_SSH_USER` with your login:

```sh
ssh YOUR_SSH_USER@wavesoft.gr
```

Confirm that the public DNS record for `wavesoft.gr` points to this server (or
to a router that forwards TCP/8090 to it). Allow inbound TCP/8090 in the hosting
firewall/security group and the server firewall. The server also needs outbound
HTTPS/WSS access to ThingPark and access to Kudzu analytics on TCP/443.

This guide uses port 8090 directly, so it can coexist with an existing website
on ports 80/443. TLS must reach the proxy with the gateway's client certificate
intact: bypass any HTTP CDN or TLS-terminating reverse proxy for this port.
The existing website certificate does not replace the proxy CA: managed CUPS
requires the listener certificate to be signed by that CA.

Run the remaining setup commands in the SSH terminal. Keep the hostname and
change the gateway EUI and ThingPark hosts as appropriate:

```sh
BS_HOST=wavesoft.gr
GW_EUI=0102030405060708
TP_CUPS_HOST=community.thingpark.io
TP_LNS_HOST=lns.thingpark.com

BS_DIR="$HOME/basicstation-proxy-test"
mkdir -p "$BS_DIR/store"
chmod 700 "$BS_DIR" "$BS_DIR/store"
cd "$BS_DIR"
umask 077
```

Use the actual gateway EUI, expressed as 16 lowercase hexadecimal characters.
The Community portal is `https://community.thingpark.io/tpe/`. The proxy's
upstream platform must match the portal containing the gateway registration.
For ThingPark Enterprise EU Dublin, the alternatives are
`thingparkenterprise.eu.actility.com` and `lns.eu.thingpark.com`; those endpoints
do not serve a Community registration. Other tenants have different endpoints; consult the
[ThingPark endpoint list](https://docs.thingpark.com/thingpark-enterprise/latest/docs/user-guide/base-stations/connect-new-base-station/authorize-base-station-to-from-tpk-core-network).

Create the private CA:

```sh
cat > ca.cnf <<'EOF'
[req]
prompt = no
distinguished_name = dn
x509_extensions = ca_ext

[dn]
CN = wavesoft.gr BasicStation Test CA

[ca_ext]
basicConstraints = critical,CA:TRUE,pathlen:0
keyUsage = critical,keyCertSign,cRLSign
subjectKeyIdentifier = hash
EOF

openssl req -new -x509 -newkey rsa:2048 -nodes \
  -keyout ca.key -out ca.crt -days 3650 -sha256 \
  -config ca.cnf
```

Create the proxy's server certificate. The gateway-facing hostname must be in
its DNS Subject Alternative Name:

```sh
openssl req -new -newkey rsa:2048 -nodes \
  -keyout server.key -out server.csr \
  -subj "/CN=$BS_HOST"

cat > server.ext <<EOF
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:$BS_HOST
EOF

openssl x509 -req -in server.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 365 -sha256 -extfile server.ext
```

Create the gateway's bootstrap client certificate:

```sh
openssl req -new -newkey rsa:2048 -nodes \
  -keyout "store/$GW_EUI.key" -out gateway.csr \
  -subj "/CN=$GW_EUI"

cat > gateway.ext <<'EOF'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature
extendedKeyUsage=clientAuth
EOF

openssl x509 -req -in gateway.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out "store/$GW_EUI.crt" -days 365 -sha256 \
  -extfile gateway.ext
```

Keep `ca.key` on the hosted server. The gateway receives only the CA certificate
and its own client certificate/key. These commands are for a fresh setup directory;
do not rerun them over an existing working CA or credential store.

## 2. Create the proxy configuration

Still in the same terminal:

```sh
GW_PIN=$(openssl x509 -in "store/$GW_EUI.crt" -outform DER |
  openssl dgst -sha256 | awk '{print $NF}')

cat > gateways.json <<EOF
{
  "version": 1,
  "gateways": [{
    "router": "$GW_EUI",
    "client_cert_sha256": ["$GW_PIN"]
  }]
}
EOF

curl -fsSLo thingpark-ca.pem \
  https://www.amazontrust.com/repository/AmazonRootCA1.pem

cat > proxy.conf <<EOF
proxy-mode=shared
cups-mode=managed

listen-host=0.0.0.0
listen-port=8090
public-uri=wss://$BS_HOST:8090
listen-tls-cert=$BS_DIR/server.crt
listen-tls-key=$BS_DIR/server.key

certs-dir=$BS_DIR/store
certs-ca-cert=$BS_DIR/ca.crt
certs-ca-key=$BS_DIR/ca.key
gateway-auth-file=$BS_DIR/gateways.json

connect-uri=wss://$TP_CUPS_HOST:443
connect-tls-ca=$BS_DIR/thingpark-ca.pem
upstream-allowed-origins=wss://$TP_LNS_HOST:443

server-side=true
client-id=REPLACE_WITH_KUDZU_CLIENT_ID
client-key=REPLACE_WITH_KUDZU_CLIENT_KEY
log-level=debug
EOF
```

Replace the two Kudzu credential placeholders in `proxy.conf` with valid
analytics credentials suitable for server-side access.

For the existing Community deployment, the expanded upstream settings are:

```ini
connect-uri=wss://community.thingpark.io:443
upstream-allowed-origins=wss://lns.thingpark.com:443
```

After correcting a platform mismatch, restart the proxy and Basic Station using
the existing CA, ingress credentials, and store. The gateway's CUPS URL remains
`https://wavesoft.gr:8090`; only the proxy's upstream destination changes.

Two settings are deliberate:

- `connect-uri` uses the ThingPark **CUPS hostname**, expressed as `wss://`.
  The current implementation derives its initial upstream CUPS URL from that
  host, using HTTPS and `/update-info`.
- `upstream-allowed-origins` permits the separate ThingPark LNS endpoint returned
  during provisioning. Add any other legitimate returned origins explicitly.

Amazon Root CA 1 is the documented CUPS trust for these SaaS examples. For
self-hosted ThingPark, substitute the platform's CA and endpoints. See
[ThingPark bootstrap configuration](https://docs.thingpark.com/thingpark-enterprise/latest/docs/user-guide/base-stations/connect-new-base-station/configure-base-station-using-basics-station).

Leave `certs-upstream-client-cert` disabled, its default. The proxy-issued client
certificate authenticates the gateway to the proxy; ThingPark-issued credentials
authenticate the proxy upstream.

## 3. Register the gateway in ThingPark

Go to **Base Stations → ADD BASE STATION**:

| Setting | Value |
|---|---|
| Manufacturer | Generic, unless an appropriate branded Basic Station profile exists |
| Model | Basics Station packet forwarder |
| Gateway ID | The gateway's actual EUI |
| LRR-UUID | `0016C0-` followed by the gateway EUI |
| RF Region | The correct regional frequency plan |
| Generate base station certificate | Enabled |

For the example EUI, the LRR-UUID is `0016C0-0102030405060708`.

For gateway EUI `24E124FFFEF37B09`, use `0016C0-24E124FFFEF37B09`.
Do not split that EUI into `24E124-FFFEF37B09`: Basics Station uses the fixed
`0016C0` prefix, not the gateway manufacturer's OUI. ThingPark documents that
LRR-UUID cannot be edited after provisioning, so a wrong UUID requires a
replacement registration. Preserve its settings before replacing it. See
[base station attributes](https://docs.thingpark.com/thingpark-enterprise/latest/docs/user-guide/base-stations/base-station-attributes/).

Do not upload the proxy CA to ThingPark. ThingPark issues its own upstream
credentials, which the proxy retrieves through CUPS. See
[ThingPark provisioning instructions](https://docs.thingpark.com/thingpark-enterprise/latest/docs/user-guide/base-stations/connect-new-base-station/provision-base-station).

If the gateway was previously provisioned and its one-time credential retrieval
has already occurred, regenerate its certificate under **Advanced → Security**
before fresh onboarding. Preserve existing working credentials first;
regeneration revokes the old certificate.

## 4. Start the proxy and configure the gateway

Use a Linux `kudzu-forwarder-bs` binary built from the Basic Station branch for
the server's architecture. Check the architecture in the SSH terminal:

```sh
uname -m
```

In a **separate local terminal**, from your analytics checkout on the Basic
Station branch, build and upload the binary. This example targets Linux x86-64
(`uname -m` reports `x86_64`); use `GOARCH=arm64` if the server reports `aarch64`:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' \
  -o /tmp/kudzu-forwarder-bs-linux ./forwarder-bs
scp /tmp/kudzu-forwarder-bs-linux \
  YOUR_SSH_USER@wavesoft.gr:basicstation-proxy-test/kudzu-forwarder-bs
```

Also download **only the gateway bootstrap files** in that local terminal so
you can upload them through the gateway UI. Substitute the actual EUI in the
two filenames:

```sh
mkdir -p "$HOME/basicstation-gateway-bootstrap"
chmod 700 "$HOME/basicstation-gateway-bootstrap"
scp YOUR_SSH_USER@wavesoft.gr:basicstation-proxy-test/ca.crt \
  YOUR_SSH_USER@wavesoft.gr:basicstation-proxy-test/store/0102030405060708.crt \
  YOUR_SSH_USER@wavesoft.gr:basicstation-proxy-test/store/0102030405060708.key \
  "$HOME/basicstation-gateway-bootstrap/"
chmod 600 "$HOME/basicstation-gateway-bootstrap/"*
```

Back in the **server SSH terminal**, start the proxy:

```sh
chmod 700 "$BS_DIR/kudzu-forwarder-bs"
"$BS_DIR/kudzu-forwarder-bs" --config="$BS_DIR/proxy.conf"
```

Keep this foreground process running for the initial verification. For tests
that must continue after you disconnect, run it under a server-side session
manager such as `tmux`, or install it under your existing service supervisor.
Use the same server account and configuration path so the private store remains
accessible.

On the gateway's Basic Station configuration page:

| Setting | Value |
|---|---|
| Server type | CUPS |
| Server URI | `https://wavesoft.gr:8090` |
| Authentication | TLS client authentication / mutual TLS |
| Trust / CA certificate | Upload `ca.crt` |
| Client certificate | Upload the downloaded `0102030405060708.crt` |
| Client private key | Upload the downloaded `0102030405060708.key` |
| Gateway EUI | The same EUI registered above |

Use the actual EUI in the filenames. Leave token/authorization fields empty for
this certificate-based setup.

For a file-based Station installation, those uploads correspond to:

```text
cups.uri    https://wavesoft.gr:8090
cups.trust  contents of ca.crt
cups.crt    contents of store/<EUI>.crt
cups.key    contents of store/<EUI>.key
```

Retain the manufacturer's `station.conf`. Stop Station before changing its
credentials. Preserve and deactivate previous configurations/fallbacks that
would connect elsewhere. See the
[Basic Station credential-file reference](https://doc.sm.tc/station/credentials.html).

Save and start Basic Station. **Leave CUPS enabled.** No ThingPark address or
certificate needs to be entered on the gateway for this managed flow.

## 5. Verify provisioning and traffic

Expected sequence:

1. The gateway calls `https://wavesoft.gr:8090/update-info`.
2. The proxy fetches ThingPark credentials and saves upstream state in `store/`.
3. The gateway receives `tc.uri` pointing to `wss://wavesoft.gr:8090`.
4. The proxy connects upstream using the ThingPark-issued credentials.
5. The gateway receives `router_config`; ThingPark shows it connected.
6. Test a real device uplink and downlink, then verify Kudzu analytics.

`Issued client cert for gateway ...` refers to the proxy-issued ingress
certificate, not a ThingPark-issued credential or a successful LNS connection.
With the connection diagnostics build, check the next CUPS poll for:

```text
CUPS upstream response status=200 bytes=...
CUPS upstream update ... cups_credential_bytes=... tc_credential_bytes=...
CUPS response sent ...
LNS discovery connecting ...
LNS discovery response sent public_origin=wss://wavesoft.gr:8090 path=/
LNS data connected ...
LNS version forwarded ...
LNS router_config forwarded ...
```

An upstream response of 14 zero bytes is a CUPS no-op. If there are no stored
upstream credentials and it returns no updates, verify the gateway registration
in the matching ThingPark tenant, its exact EUI/LRR-UUID, and certificate bootstrap
state. The proxy can still issue its own credentials in this case. Do not delete
the CA or store, or send an ad hoc upstream bootstrap request: credential retrieval
may be one-time. The diagnostic logs report metadata without printing credentials.

The bootstrap fix reports an empty upstream `tcUri` until ThingPark supplies
an LNS endpoint. While that endpoint is unavailable, expect `LNS discovery
waiting for upstream CUPS provisioning`. Discovery should not contact
`community.thingpark.io`: that is the CUPS service in this setup.

The gateway-facing discovery data URI must contain a root slash:
`wss://wavesoft.gr:8090/?router=24e124fffef37b09`. A URI such as
`wss://wavesoft.gr:8090?router=24e124fffef37b09` is rejected by Station before it
opens a data connection. The URI fix inserts that slash automatically; keep
the existing `public-uri` and gateway CUPS URL unchanged.

For a session that goes offline after appearing connected, keep the complete
log through the failure instead of repeatedly restarting. The keepalive build
logs `gateway` or `upstream WebSocket read ended`, its reason, and session age.
Each minute it also reports active sessions, forwarded handshake messages, and
per-peer ping/pong counters. `read_timeout` near 60 seconds identifies the read
inactivity deadline; `websocket_close_1006` means an abrupt stream ending and
does not identify its underlying cause. A successful WebSocket reconnect must
be followed by a fresh `version` and `router_config` exchange.

The keepalive fix refreshes deadlines for application messages and incoming
ping frames as well as pong replies, while retaining timeouts for silent peers.
Verify stability for several minutes, then test a known uplink and an actual
device downlink. The analytics delivery counters do not count LNS disconnects.

| Error | Check |
|---|---|
| Gateway cannot reach the proxy | DNS resolves to the hosted server, TCP/8090 is allowed/forwarded, and the proxy process is running |
| Gateway TLS verification failure | Use `wavesoft.gr`, confirm DNS SAN `wavesoft.gr`, and upload the proxy's `ca.crt` |
| Gateway authorization rejected | Same EUI and correct gateway client certificate |
| Upstream CUPS request rejected | ThingPark registration and certificate bootstrap state |
| Unapproved CUPS/discovery endpoint | Add the exact returned ThingPark origin to `upstream-allowed-origins`, then restart the proxy |
| Upstream TLS verification failure | Correct ThingPark CA and endpoint |

Keep the server's `store/` directory and CA between runs: they hold the ThingPark
credentials and provisioning state. If the server IP changes, update DNS and
any firewall/NAT rules; the URLs and DNS-based certificate can stay the same
while the hostname remains `wavesoft.gr`. If the hostname changes, update
`public-uri` and the gateway CUPS URL, and reissue the server certificate with
the new DNS SAN using the existing CA. Renew the server certificate before its
365-day validity expires.
