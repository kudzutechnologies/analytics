package client

import (
	"github.com/kudzutechnologies/analytics/api"
	"google.golang.org/grpc"
)

// SetTestConnection wires an already-authenticated gRPC connection for unit tests.
func SetTestConnection(c *Client, conn *grpc.ClientConn, rpc api.AnalyticsServerClient, token string) {
	c.conn = conn
	c.client = rpc
	c.sessionToken = token
}
