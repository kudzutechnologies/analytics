# Kudzu Analytics Forwarder

> Semtech UDP Proxy for extracting LoRaWAN Analytics

This directory contains the sources for the Kudzu Analytics Forwarder that can be placed in the communication channel between the Semtech UDP Packet Forarder and the rest of the LoRaWAN infrastructure in order to collect analytics meta-data.

![Analytics](https://kudzu.gr/images/solutions/analytics-infra-1.svg)

## Installation

You can find pre-built binaries for different CPU architectures on the assets of the [Releases Page](https://github.com/kudzutechnologies/analytics/releases). Each tar archive contains a binary file with the proxy executable.

For example, to install the binary for the ARM7 CPU architecture (on a linux platform) you can do the following:

```sh
# Download and extract
curl -L -o kudzu-forwarder.tgz https://github.com/kudzutechnologies/analytics/releases/download/v0.1.10/kudzu-forwarder-arm7.tgz
tar -zxf kudzu-forwarder.tgz

# Move the binary to the desired folder
mv kudzu-forwarder-arm7 /usr/bin/kudzu-forwarder
```

## Configuration

The forwarder can be configured with a configuration file. Typically this file is located at `/etc/kudzu-forwarder.conf`.

An example of such configuration file is the following:

```ini
# ======================================================
# Configuration file for Kudzu Analytics Forwarder
# ======================================================

# The following parameters are obtained from the Kudzu Analytics
# platform https://analytics.v2.kudzu.gr/ and is typically shared
# between all of the gateways of the same customer
client-id="<api-client-id>"
client-key="<api-client-key>"

# The following parameter is obtained from the Kudzu Analytics
# patform and is unique for every gateway.
gateway="<platform-gateway-id>"

# ======================================================
# Configure this to point to the LoRaWAN Server
# ======================================================

# The hostname of the server where to connect to
connect-host=eu1.cloud.thethings.network

# The Uplink/Downlink ports to connect to
connect-port-up=1700
connect-port-down=1700

# ======================================================
# Configure this to match the expected endpoint from
# the lora forarder service
# ======================================================

# The hostname where to listen for incoming connections
listen-host=127.0.0.1

# The Uplink/Downlink ports to listen on
listen-port-up=1800
listen-port-down=1800
```

You can then launch the client using:

```sh
/usr/bin/kudzu-forwarder --config=/etc/kudzu-forwarder.conf
```

### Configuration Options

| Parameter Name | Required | Default | Description |
|---|---|---|---|
| **analytics-connect-timeout** | | `0` |  how long to wait for analytics connection |
| **analytics-endpoint** | | `""` |  the analytics endpoint to push the data to |
| **analytics-max-backoff** | | `0` |  the maximum time to wait for reconnecting |
| **analytics-request-timeout** | | `0` |  how long to wait for analytics to be pushed |
| **buffer-size** | | `1500` |  how much memory to allocate for the UDP packets |
| **client-id** | 🔴 | `""` |  the client ID to use for connecting to Kudzu Analytics |
| **client-key** | 🔴 | `""` |  the private client key to use for connecting to Kudzu Analytics |
| **config** | | `""` |  path to the configuration file |
| **connect-host** | 🔴 | `""` |  the hostname where to connect to (the LoRa Server) |
| **connect-interface** | | `"0.0.0.0"` |  the interface to bind when connecting to remote host |
| **connect-port-down** | | `1700` |  the (local) port where to receive downlink datagrams from |
| **connect-port-up** | | `1700` |  the server port where to send uplink datagrams to |
| **connect-retry-interval** | | `1` |  how many seconds to wait before re-connecting to the remote server if the connection is severed |
| **debug-dump** | | `""` |  the filename where to write the traffic for debugging |
| **flush-interval** | | `0` |  how frequently to flush collected metrics to analytics |
| **gateway** | 🔴 | `""` |  the ID of the gateway the forwarder is pushing data for |
| **gauge-stat** | | `false` |  the statistics are gauge values |
| **listen-host** | | `"127.0.0.1"` |  the hostname where to listen (UDP forwarder connects here) |
| **listen-port-down** | | `1801` |  the UDP forwarder port where to send downlink datagrams to |
| **listen-port-up** | | `1800` |  the (local) port where to receive uplink datagrams from the UDP forwarder |
| **log-file** | | `""` |  writes the program output to the specified logfile |
| **log-level** | | `"info"` |  selects the verbosity of logging, can be 'error', 'warn', 'info', 'debug' |
| **max-udp-streams** | | `0` |  how many distinct UDP streams to maintain. Only useful on server-side mode |
| **queue-size** | | `100` |  how many items to keep in the queue |
| **server-side** | | `false` |  the forwarder runs on the server-side |
| **version** | | `false` |  show the package version and exit |

### Alternative Configuration Ways

While the configuration file is the default way of configuring the client you can also configure it using environment variables or command-line arguments:

For example:

```sh
/usr/bin/kudzu-forwarder \
  -client-id="<api-client-id>" \
  -client-key="<api-client-key>" \
  -gateway="<platform-gateway-id>" \
  -connect-host=eu1.cloud.thethings.network \
  -connect-port-up=1700 \
  -connect-port-down=1700 \
  -listen-host=127.0.0.1 \
  -listen-port-up=1800 \
  -listen-port-down=1800
```

Or even:

```sh
export client_id="<api-client-id>"
export client_key="<api-client-key>"
export gateway="<platform-gateway-id>"
export connect_host=eu1.cloud.thethings.network
export connect_port_up=1700
export connect_port_down=1700
export listen_host=127.0.0.1
export listen_port_up=1800
export listen_port_down=1800
/usr/bin/kudzu-forwarder
```

## Build Instructions

To build the analytics forwarder from source, you only need a compatible go version:

  * [Go Lang](https://go.dev/doc/install) 1.18 or newer

You can then build from the sources in the `forwarder` folder:

```sh
# Get the sources
git clone https://github.com/kudzutechnologies/analytics.git
cd analytics
cd forwarder

# Build the forwarder static binary for the desired OS and architecture, eg:
GOOS=linux GOARCH=arm GOARM=6 go build \
  -o forwarder-linux-arm6 \
  -a -gcflags=all="-l -B" \
  -ldflags="-s -w"
```

Go has a built-in toolchain for many different CPU and OS arhitectures. To list the ones
available in your current environment you can use:

```
go tool dist list
```