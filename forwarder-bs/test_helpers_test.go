package forwarderbs

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kudzutechnologies/analytics/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func wsURL(s string) string { return "ws" + strings.TrimPrefix(s, "http") }

// Each test owns its listener, certificate and backend; no real gateway or API
// credentials are needed. Handlers can block until ctx.Done to test shutdown.
type analyticsFixture struct {
	api.UnimplementedAnalyticsServerServer
	push func(context.Context, *api.AnalyticsMetrics) (*api.RespPush, error)
}

func (*analyticsFixture) Hello(context.Context, *api.ReqHello) (*api.RespHello, error) {
	return &api.RespHello{Challenge: []byte("fixture-challenge")}, nil
}
func (*analyticsFixture) Login(context.Context, *api.ReqLogin) (*api.RespLogin, error) {
	return &api.RespLogin{AccessToken: "fixture-session"}, nil
}
func (f *analyticsFixture) PushMetrics(ctx context.Context, m *api.AnalyticsMetrics) (*api.RespPush, error) {
	if f.push != nil {
		return f.push(ctx, m)
	}
	return &api.RespPush{}, nil
}

func startAnalyticsFixture(t *testing.T, f *analyticsFixture) (endpoint, caFile string) {
	t.Helper()
	certServer := httptest.NewTLSServer(nil)
	cert := certServer.TLS.Certificates[0]
	certServer.Close()
	caFile = filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})))
	api.RegisterAnalyticsServerServer(s, f)
	go s.Serve(l)
	t.Cleanup(func() { s.Stop(); l.Close() })
	return l.Addr().String(), caFile
}

func fixtureCertStore(dir, cert, key string) (*CertStore, error) {
	if cert == "" && key == "" {
		if _, _, e := loadOrCreateCA(dir); e != nil {
			return nil, e
		}
	}
	return NewCertStore(dir, cert, key)
}
