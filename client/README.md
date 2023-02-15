<!-- Code generated by gomarkdoc. DO NOT EDIT -->

# client

```go
import "github.com/kudzutechnologies/analytics/client"
```

### Kudzu RPC Client for pushing analytics data

This package implements the gRPC analytics client package that can be used for implementing embedded analytics forwardrs to the Kudzu Analytis platform.

<details><summary>Example</summary>
<p>

```go
package main

import (
	"github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
)

func main() {
	// Create a client
	c := client.CreateAnalyticsClient(client.AnalyticsClientConfig{
		ClientId:  "1122334455667788",
		ClientKey: "11223344556677889900aabbccddeeff",
	})

	// Connect to the server
	err := c.Connect()
	if err != nil {
		panic(err)
	}

	// Push analytics data
	metrics := &api.AnalyticsMetrics{}
	err = c.PushMetrics(metrics)
	if err != nil {
		panic(err)
	}

	// Disconnect the client
	c.Disconnect()
}
```

</p>
</details>

## Index

- [Variables](<#variables>)
- [type AnalyticsClientConfig](<#type-analyticsclientconfig>)
- [type Client](<#type-client>)
  - [func CreateAnalyticsClient(config AnalyticsClientConfig) *Client](<#func-createanalyticsclient>)
  - [func (c *Client) Connect() error](<#func-client-connect>)
  - [func (c *Client) Disconnect() error](<#func-client-disconnect>)
  - [func (c *Client) PushMetrics(metrics *api.AnalyticsMetrics) error](<#func-client-pushmetrics>)


## Variables

```go
var (
    // An error thrown when trying to use the client while not connected
    NotConnectedError = fmt.Errorf("Client is not connnected")
)
```

## type AnalyticsClientConfig

Configuration parameters that can be passed to the analytics client

```go
type AnalyticsClientConfig struct {
    // The API client ID & Key for signing in
    ClientId  string `json:"client_id"`
    ClientKey string `json:"client_key"`

    // The endpoint to use for uploading the data (Optional)
    Endpoint string `json:"endpoint,omitempty"`
    // The server CA certificate file to use for validating the connection (Optional)
    CAFile string `json:"ca_file,omitempty"`
    // The default timeout for connecting (seconds)
    ConnectTimeout int32 `json:"connect_timeout,omitempty"`
    // The default timeout for all the requests (seconds)
    RequestTimeout int32 `json:"request_timeout,omitempty"`
    // The maximum re-connection back-off (seconds)
    MaxReconnectBackoff int32 `json:"max_reconnect_backoff,omitempty"`
    // Wether or not to automatically re-connect to the server
    AutoReconnect *bool `json:"reconnect,omitempty"`
}
```

## type Client

the RPC client

```go
type Client struct {
    // contains filtered or unexported fields
}
```

### func CreateAnalyticsClient

```go
func CreateAnalyticsClient(config AnalyticsClientConfig) *Client
```

#### Create an instance of the analytics client

The client will not be connected until you call the .Connect method.

### func \(\*Client\) Connect

```go
func (c *Client) Connect() error
```

### func \(\*Client\) Disconnect

```go
func (c *Client) Disconnect() error
```

### func \(\*Client\) PushMetrics

```go
func (c *Client) PushMetrics(metrics *api.AnalyticsMetrics) error
```

Pushes analyics metrics to the service



Generated by [gomarkdoc](<https://github.com/princjef/gomarkdoc>)