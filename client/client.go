package client

import (
	context "context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/kudzutechnologies/analytics/api"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Revision:
// v1 - First public release of the client
// v2 - Added support for multiple antennas
// v3 - Changed the payload structure to accommodate older compilers
const ClientVersion = 3

var defaultEndpoint string = "analytics.v2.kudzu.gr:50051"

var (
	// An error thrown when trying to use the client while not connected
	ErrNotConnected = fmt.Errorf("client is not connnected")
)

// Configuration parameters that can be passed to the analytics client
type AnalyticsClientConfig struct {
	// The API client ID & Key for signing in
	ClientId  string `json:"client_id"`
	ClientKey string `json:"client_key"`

	// The endpoint to use for uploading the data (Optional)
	Endpoint string `json:"endpoint,omitempty"`
	// Optional CA PEM file appended to the system trust store (additive).
	// When empty, only system roots are used.
	CAFile string `json:"ca_file,omitempty"`
	// The default timeout for connecting (seconds)
	ConnectTimeout int32 `json:"connect_timeout,omitempty"`
	// The default timeout for all the requests (seconds)
	RequestTimeout int32 `json:"request_timeout,omitempty"`
	// The maximum re-connection back-off (seconds)
	MaxReconnectBackoff int32 `json:"max_reconnect_backoff,omitempty"`
	// Wether or not to automatically re-connect to the server
	AutoReconnect *bool `json:"reconnect,omitempty"`
	// Indicates that we are forwarding data from the server-side
	ServerSide *bool `json:"server_side,omitempty"`
}

// the RPC client
type Client struct {
	client       api.AnalyticsServerClient
	config       AnalyticsClientConfig
	conn         *grpc.ClientConn
	reqTimeout   time.Duration
	connTimeout  time.Duration
	sessionToken string
}

// loadRootCAs returns the system certificate pool, optionally appending the
// PEM certificates from caFile. When caFile is empty, only system roots are used.
// When caFile is set, it is loaded in addition to the system roots and must parse
// successfully (additive trust; explicit CA is required to be valid).
func loadRootCAs(caFile string) (*x509.CertPool, error) {
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("failed to load system root CAs: %w", err)
	}
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	if caFile == "" {
		return rootCAs, nil
	}

	pemServerCA, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA file: %w", err)
	}
	if !rootCAs.AppendCertsFromPEM(pemServerCA) {
		return nil, fmt.Errorf("failed to add server CA's certificate")
	}
	return rootCAs, nil
}

// loadTLSConfig builds a TLS config using system roots, plus an optional CA file.
func loadTLSConfig(caFile string, nextProtos []string) (*tls.Config, error) {
	rootCAs, err := loadRootCAs(caFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		RootCAs:    rootCAs,
		MinVersion: tls.VersionTLS12,
		NextProtos: nextProtos,
	}, nil
}

func loadTLSCredentials(cc *AnalyticsClientConfig) (credentials.TransportCredentials, error) {
	config, err := loadTLSConfig(cc.CAFile, []string{"h2"})
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(config), nil
}

// Create an instance of the analytics client
//
// The client will not be connected until you call the .Connect method.
func CreateAnalyticsClient(config AnalyticsClientConfig) *Client {
	// Default request time-out
	reqTimeout := time.Duration(0)
	if config.RequestTimeout != 0 {
		reqTimeout = time.Second * time.Duration(config.RequestTimeout)
	}

	// Default connection time-out
	connTimeout := time.Second * 30
	if config.ConnectTimeout != 0 {
		connTimeout = time.Second * time.Duration(config.ConnectTimeout)
	}

	return &Client{
		client:      nil,
		config:      config,
		connTimeout: connTimeout,
		reqTimeout:  reqTimeout,
	}
}

func (c *Client) Disconnect() error {
	if c.conn == nil {
		return ErrNotConnected
	}

	con := c.conn
	c.conn = nil
	c.client = nil

	return con.Close()
}

func (c *Client) Connect() error {
	if c.conn != nil {
		c.Disconnect()
	}

	tlsCredentials, err := loadTLSCredentials(&c.config)
	if err != nil {
		return fmt.Errorf("could not load CA certificate: %w", err)
	}

	endpoint := defaultEndpoint
	if c.config.Endpoint != "" {
		endpoint = c.config.Endpoint
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.connTimeout)
	defer cancel()

	// Dial to the remote endpoint
	conn, err := grpc.DialContext(ctx, endpoint, grpc.WithTransportCredentials(tlsCredentials), grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("could not connect to server: %w", err)
	}

	// Create a client for logging in
	client := api.NewAnalyticsServerClient(conn)

	// Send hello & get login challenge
	clientId, err := hex.DecodeString(c.config.ClientId)
	if err != nil {
		return fmt.Errorf("invalid client ID")
	}

	helloResp, err := client.Hello(ctx, &api.ReqHello{
		Version: ClientVersion,
	})
	if err != nil {
		return fmt.Errorf("could not handshake with server: %w", err)
	}

	// Use hello challenge to login
	clientKey, err := hex.DecodeString(c.config.ClientKey)
	if err != nil {
		return fmt.Errorf("invalid client key")
	}
	b := append(append(helloResp.Challenge, '|'), clientKey...)
	serverSide := false
	if c.config.ServerSide != nil {
		serverSide = *c.config.ServerSide
	}
	loginResp, err := client.Login(ctx, &api.ReqLogin{
		ClientId:   clientId,
		Hash:       sha256.New().Sum(b),
		ServerSide: serverSide,
	})
	if err != nil {
		return fmt.Errorf("could not login: %w", err)
	}

	// Store the new client parameters
	c.client = client
	c.conn = conn
	c.sessionToken = loginResp.AccessToken
	return nil
}

func (c *Client) withReconnect(fn func() error) error {
	var err error
	backoff := time.Second * 1
	maxBackoff := time.Minute
	if c.config.MaxReconnectBackoff != 0 {
		maxBackoff = time.Second * time.Duration(c.config.MaxReconnectBackoff)
	}

	// If re-connect is disabled, don't bother
	if c.config.AutoReconnect != nil && !*c.config.AutoReconnect {
		return fn()
	}

	// Otherwise run the function in a reconnection loop
	for {
		if c.conn == nil {
			// If not connected, try to connect
			err = c.Connect()
		} else {
			// Otherwise try to use the function
			err = fn()
		}

		if err != nil {
			code := status.Code(err)
			if code == codes.DeadlineExceeded || code == codes.Unavailable || errors.Is(err, context.DeadlineExceeded) {
				// Sleep for back-off duration and try to re-connect
				time.Sleep(backoff)
				backoff += backoff * 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}

				// Connect again
				c.Disconnect()
				continue
			}
		}
		return err
	}
}

func (c *Client) createContext() (context.Context, context.CancelFunc) {
	var cancel context.CancelFunc = func() {}
	ctx := context.Background()
	if c.reqTimeout != 0 {
		ctx, cancel = context.WithTimeout(context.Background(), c.reqTimeout)
	}

	md := metadata.Pairs("token", c.sessionToken)
	return metadata.NewOutgoingContext(ctx, md), cancel
}

// Pushes analyics metrics to the service
func (c *Client) PushMetrics(metrics *api.AnalyticsMetrics) error {
	if c.conn == nil {
		return ErrNotConnected
	}

	ctx, cancel := c.createContext()
	defer cancel()

	return c.withReconnect(func() error {
		_, err := c.client.PushMetrics(ctx, metrics)
		if err != nil {
			return err
		}

		return nil
	})
}

// GatewayUpsert creates or updates a gateway record on the analytics service.
func (c *Client) GatewayUpsert(req *api.ReqGatewayUpsert) (*api.RespGatewaySync, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}

	ctx, cancel := c.createContext()
	defer cancel()

	var resp *api.RespGatewaySync
	err := c.withReconnect(func() error {
		var err error
		resp, err = c.client.GatewayUpsert(ctx, req)
		return err
	})
	return resp, err
}

// GatewayDelete removes a gateway record controlled by the ingress_api integration.
func (c *Client) GatewayDelete(req *api.ReqGatewayDelete) (*api.RespGatewaySync, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}

	ctx, cancel := c.createContext()
	defer cancel()

	var resp *api.RespGatewaySync
	err := c.withReconnect(func() error {
		var err error
		resp, err = c.client.GatewayDelete(ctx, req)
		return err
	})
	return resp, err
}
