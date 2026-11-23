package forwarderbs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"github.com/kudzutechnologies/analytics/api"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRecordPersistenceAndPatch(t *testing.T) {
	ctx := context.Background()
	s := &FilesystemPersistence{Dir: t.TempDir()}
	id := "0102030405060708"
	if _, e := s.Load(ctx, id); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	rev, e := s.Save(ctx, GatewayRecord{EUI: id, CUPS: []byte("old")}, 0)
	if e != nil || rev != 1 {
		t.Fatal(rev, e)
	}
	if _, e = s.Save(ctx, GatewayRecord{EUI: id}, 0); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	var patch GatewayRecordPatch
	if e = json.Unmarshal([]byte(`{"tc":"bmV3"}`), &patch); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Patch(ctx, id, patch, rev); e != nil {
		t.Fatal(e)
	}
	r, e := s.Load(ctx, id)
	if e != nil || string(r.CUPS) != "old" || string(r.TC) != "new" {
		t.Fatal(r, e)
	}
	empty := []byte{}
	if _, e = s.Patch(ctx, id, GatewayRecordPatch{CUPS: &empty}, r.Revision); e != nil {
		t.Fatal(e)
	}
	r, _ = s.Load(ctx, id)
	if len(r.CUPS) != 0 || string(r.TC) != "new" {
		t.Fatal(r)
	}
	if _, e = s.Load(ctx, "../../escape"); e == nil {
		t.Fatal("invalid EUI accepted")
	}
}
func TestFilesystemRecordErrors(t *testing.T) {
	dir := t.TempDir()
	s := &FilesystemPersistence{Dir: dir}
	id := "0102030405060708"
	ctx := context.Background()
	if _, err := s.Load(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".gateway.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(ctx, id); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid record treated as missing: %v", err)
	}
	if _, err := s.Save(ctx, GatewayRecord{EUI: id}, 0); err == nil {
		t.Fatal("invalid record overwritten")
	}
	s.ReadOnly = true
	if _, err := s.Save(ctx, GatewayRecord{EUI: id}, 0); !errors.Is(err, ErrReadOnly) {
		t.Fatal(err)
	}
}

func TestBootstrapExportAndParsing(t *testing.T) {
	store, e := fixtureCertStore(t.TempDir(), "", "")
	if e != nil {
		t.Fatal(e)
	}
	c := DefaultForwarderConfig()
	c.CertsCACert = filepath.Join(store.IssuerConfig.CertsDir, "ca.crt")
	c.CertsCAKey = filepath.Join(store.IssuerConfig.CertsDir, "ca.key")
	c.BootstrapGW = "01-02-03-04-05-06-07-08"
	c.BootstrapDir = filepath.Join(t.TempDir(), "bootstrap")
	if e = ExportGatewayBootstrap(c); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(c.BootstrapDir, "0102030405060708")
	trust, _ := os.ReadFile(filepath.Join(dir, "cups.trust"))
	original, _ := os.ReadFile(c.CertsCACert)
	if string(trust) != string(original) {
		t.Fatal("trust changed")
	}
	cert, _ := os.ReadFile(filepath.Join(dir, "cups.crt"))
	key, _ := os.ReadFile(filepath.Join(dir, "cups.key"))
	if _, e = tls.X509KeyPair(cert, key); e != nil {
		t.Fatal(e)
	}
	if e = ExportGatewayBootstrap(c); e == nil {
		t.Fatal("overwrote output")
	}
	c.CertsCAKey = ""
	if _, e = IssueGatewayBootstrap(c, c.BootstrapGW); !errors.Is(e, ErrIssuanceDisabled) {
		t.Fatal(e)
	}
	config := filepath.Join(t.TempDir(), "proxy.conf")
	os.WriteFile(config, []byte("issuer-cert=config.crt\nissuer-key=config.key\n"), 0600)
	parsed, e := ParseForwarderConfig([]string{"--config=" + config, "--bootstrap-gw=0102030405060708", "--issuer-key=cli.key"}, []string{"ISSUER_CERT=env.crt"})
	if e != nil || parsed.IssuerCert != "env.crt" || parsed.IssuerKey != "cli.key" {
		t.Fatal(parsed, e)
	}
}
func TestSeparateIssuerWithoutRootKey(t *testing.T) {
	store, e := fixtureCertStore(t.TempDir(), "", "")
	if e != nil {
		t.Fatal(e)
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	issuer := &x509.Certificate{SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: "Intermediate"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, issuer, store.caCert, &key.PublicKey, fixtureCAKey(store))
	if e != nil {
		t.Fatal(e)
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	trust, _ := os.ReadFile(filepath.Join(store.IssuerConfig.CertsDir, "ca.crt"))
	c := DefaultForwarderConfig()
	c.CertsCACertPEM = trust
	c.IssuerCertPEM = pemCertificate(der)
	c.IssuerKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})
	b, e := IssueGatewayBootstrap(c, "0102030405060708")
	if e != nil {
		t.Fatal(e)
	}
	pair, e := tls.X509KeyPair(b.Cert, b.Key)
	if e != nil || len(pair.Certificate) != 2 {
		t.Fatal(e, len(pair.Certificate))
	}
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	if leaf.NotAfter.After(issuer.NotAfter) {
		t.Fatal("validity exceeds issuer")
	}
}

type memoryPersistence struct {
	mu      sync.Mutex
	records map[string]GatewayRecord
}

func (m *memoryPersistence) Load(ctx context.Context, id string) (GatewayRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok {
		return GatewayRecord{}, ErrNotFound
	}
	return r, nil
}
func (m *memoryPersistence) Save(ctx context.Context, r GatewayRecord, rev uint64) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records[r.EUI].Revision != rev {
		return 0, ErrConflict
	}
	r.Revision = rev + 1
	m.records[r.EUI] = r
	return r.Revision, nil
}

type discardAnalytics struct{}

func (discardAnalytics) ConnectContext(context.Context) error                         { return nil }
func (discardAnalytics) PushMetricsOnce(context.Context, *api.AnalyticsMetrics) error { return nil }
func (discardAnalytics) Disconnect() error                                            { return nil }
func TestEmbeddedLifecycle(t *testing.T) {
	c := DefaultForwarderConfig()
	c.ConnectURI = "ws://127.0.0.1:1"
	c.GatewayId = "fixture"
	c.GatewayEUI = "0102030405060708"
	c.ListenPort = 0
	for _, owned := range []bool{false, true} {
		f, e := New(c, Dependencies{Persistence: &memoryPersistence{records: map[string]GatewayRecord{}}, Analytics: discardAnalytics{}})
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithCancel(context.Background())
		if owned {
			done := make(chan error, 1)
			go func() { done <- f.Run(ctx) }()
			time.Sleep(20 * time.Millisecond)
			cancel()
			select {
			case e = <-done:
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(time.Second):
				t.Fatal("runner did not stop")
			}
		} else {
			if e = f.Start(ctx); e != nil {
				t.Fatal(e)
			}
			srv := httptest.NewServer(f.Handler())
			resp, e := http.Get(srv.URL + "/update-info")
			if e != nil {
				t.Fatal(e)
			}
			resp.Body.Close()
			srv.Close()
			cancel()
			if e = f.Shutdown(context.Background()); e != nil {
				t.Fatal(e)
			}
		}
		if e = f.Start(context.Background()); e == nil {
			t.Fatal("restarted stopped instance")
		}
	}
}
