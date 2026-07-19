package client_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"testing"

	"github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const (
	testClientID  = "aabbccddeeff001122334455"
	testClientKey = "0123456789abcdef0123456789abcdef0123456789abcdef"
	testToken     = "test-access-token"
)

type mockAnalyticsServer struct {
	api.UnimplementedAnalyticsServerServer

	lastUpsert *api.ReqGatewayUpsert
	lastDelete *api.ReqGatewayDelete
	lastToken  string
	upsertResp *api.RespGatewaySync
	deleteResp *api.RespGatewaySync
	upsertErr  error
	deleteErr  error
}

func (m *mockAnalyticsServer) Hello(ctx context.Context, req *api.ReqHello) (*api.RespHello, error) {
	return &api.RespHello{
		Revision:  1,
		Challenge: []byte("challenge"),
	}, nil
}

func (m *mockAnalyticsServer) Login(ctx context.Context, req *api.ReqLogin) (*api.RespLogin, error) {
	expectID, _ := hex.DecodeString(testClientID)
	if hex.EncodeToString(req.ClientId) != hex.EncodeToString(expectID) {
		return nil, status.Error(codes.Unauthenticated, "bad client id")
	}
	key, _ := hex.DecodeString(testClientKey)
	b := append(append([]byte("challenge"), '|'), key...)
	expect := sha256.New().Sum(b)
	if hex.EncodeToString(req.Hash) != hex.EncodeToString(expect) {
		return nil, status.Error(codes.Unauthenticated, "bad hash")
	}
	return &api.RespLogin{AccessToken: testToken}, nil
}

func (m *mockAnalyticsServer) PushMetrics(ctx context.Context, in *api.AnalyticsMetrics) (*api.RespPush, error) {
	return &api.RespPush{}, nil
}

func (m *mockAnalyticsServer) GatewayUpsert(ctx context.Context, in *api.ReqGatewayUpsert) (*api.RespGatewaySync, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("token"); len(vals) > 0 {
			m.lastToken = vals[0]
		}
	}
	m.lastUpsert = in
	if m.upsertErr != nil {
		return nil, m.upsertErr
	}
	if m.upsertResp != nil {
		return m.upsertResp, nil
	}
	return &api.RespGatewaySync{GatewayId: "deadbeefdeadbeefdeadbeef", Applied: true}, nil
}

func (m *mockAnalyticsServer) GatewayDelete(ctx context.Context, in *api.ReqGatewayDelete) (*api.RespGatewaySync, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("token"); len(vals) > 0 {
			m.lastToken = vals[0]
		}
	}
	m.lastDelete = in
	if m.deleteErr != nil {
		return nil, m.deleteErr
	}
	if m.deleteResp != nil {
		return m.deleteResp, nil
	}
	return &api.RespGatewaySync{GatewayId: "deadbeefdeadbeefdeadbeef", Applied: true}, nil
}

func startMockServer(t *testing.T, srv *mockAnalyticsServer) (*client.Client, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	gs := grpc.NewServer()
	api.RegisterAnalyticsServerServer(gs, srv)
	go func() {
		_ = gs.Serve(lis)
	}()

	// Dial via bufconn by overriding Connect path: create client then manually wire.
	// The production Connect dials TLS; for unit tests we inject a connected client.
	c := client.CreateAnalyticsClient(client.AnalyticsClientConfig{
		ClientId:      testClientID,
		ClientKey:     testClientKey,
		AutoReconnect: boolPtr(false),
	})

	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Perform Hello/Login manually so session token is set the same way Connect does.
	rpc := api.NewAnalyticsServerClient(conn)
	hello, err := rpc.Hello(context.Background(), &api.ReqHello{Version: client.ClientVersion})
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	clientID, _ := hex.DecodeString(testClientID)
	clientKey, _ := hex.DecodeString(testClientKey)
	b := append(append(hello.Challenge, '|'), clientKey...)
	login, err := rpc.Login(context.Background(), &api.ReqLogin{
		ClientId: clientID,
		Hash:     sha256.New().Sum(b),
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Use unexported fields via a test helper package pattern:
	// wrap by calling Connect against a real TLS endpoint won't work;
	// instead expose a test-only setup through PushMetrics path by using reflection-free
	// exported test seam: createAnalyticsClientForTest is not available, so we
	// set connection using a small package-level helper in client_test_helpers.
	client.SetTestConnection(c, conn, rpc, login.AccessToken)

	cleanup := func() {
		_ = c.Disconnect()
		gs.Stop()
		_ = lis.Close()
	}
	return c, cleanup
}

func boolPtr(v bool) *bool { return &v }

func TestGatewayUpsertSendsTokenAndRequest(t *testing.T) {
	srv := &mockAnalyticsServer{}
	c, cleanup := startMockServer(t, srv)
	defer cleanup()

	eid := "edge-gw-1"
	name := "My Gateway"
	eui, _ := hex.DecodeString("0102030405060708")
	resp, err := c.GatewayUpsert(&api.ReqGatewayUpsert{
		ExternalId: &eid,
		Name:       &name,
		GatewayEui: eui,
	})
	if err != nil {
		t.Fatalf("GatewayUpsert: %v", err)
	}
	if !resp.Applied {
		t.Fatalf("expected applied=true")
	}
	if srv.lastToken != testToken {
		t.Fatalf("token=%q want %q", srv.lastToken, testToken)
	}
	if srv.lastUpsert == nil || srv.lastUpsert.GetExternalId() != eid {
		t.Fatalf("unexpected upsert request: %+v", srv.lastUpsert)
	}
	if srv.lastUpsert.GetName() != name {
		t.Fatalf("name=%q want %q", srv.lastUpsert.GetName(), name)
	}
}

func TestGatewayDeletePropagatesPermissionDenied(t *testing.T) {
	srv := &mockAnalyticsServer{
		deleteErr: status.Error(codes.PermissionDenied, "modify_gateways required"),
	}
	c, cleanup := startMockServer(t, srv)
	defer cleanup()

	eid := "edge-gw-1"
	_, err := c.GatewayDelete(&api.ReqGatewayDelete{ExternalId: &eid})
	if err == nil {
		t.Fatal("expected error")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code=%v want PermissionDenied", status.Code(err))
	}
}

func TestGatewayUpsertRequiresConnection(t *testing.T) {
	c := client.CreateAnalyticsClient(client.AnalyticsClientConfig{
		ClientId:  testClientID,
		ClientKey: testClientKey,
	})
	eid := "x"
	_, err := c.GatewayUpsert(&api.ReqGatewayUpsert{ExternalId: &eid})
	if err != client.ErrNotConnected {
		t.Fatalf("err=%v want ErrNotConnected", err)
	}
}
