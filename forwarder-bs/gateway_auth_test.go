package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func gatewayCertificate(t *testing.T, cn string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestSharedGatewayProofCannotSelectAnotherIdentity(t *testing.T) {
	a, b := gatewayCertificate(t, "arbitrary-cn"), gatewayCertificate(t, "another-cn")
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("fixture-token"), 0600); err != nil {
		t.Fatal(err)
	}
	pin := func(c tls.Certificate) string { v := sha256.Sum256(c.Certificate[0]); return hex.EncodeToString(v[:]) }
	manifest := map[string]interface{}{"version": 1, "gateways": []map[string]interface{}{
		{"router": "0102030405060708", "client_cert_sha256": []string{pin(a)}, "ingress_token_file": token, "upstream_tc_token_file": token},
		{"router": "1122334455667788", "client_cert_sha256": []string{pin(b)}},
	}}
	raw, _ := json.Marshal(manifest)
	path := filepath.Join(dir, "access.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var calls int32
	auth := make(chan string, 4)
	up := websocket.Upgrader{}
	lns := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		auth <- r.Header.Get("Authorization")
		c, e := up.Upgrade(w, r, nil)
		if e == nil {
			defer c.Close()
			c.ReadMessage()
		}
	}))
	defer lns.Close()
	p, err := NewWSProxy(&WSProxyConfig{ProxyMode: "shared", GatewayAuthFile: path, MaxStreams: 2, ConnectURI: wsURL(lns.URL)})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(p.server.Handler)
	srv.TLS = &tls.Config{ClientAuth: tls.RequestClientCert, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, Certificates: []tls.Certificate{a}}}
	if c, _, e := dialer.Dial(wsURL(srv.URL)+"/?router=1122334455667788", nil); e == nil {
		c.Close()
		t.Fatal("gateway A selected B")
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("unauthorized request reached upstream")
	}
	c, _, err := dialer.Dial(wsURL(srv.URL)+"/?router=0102030405060708", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	select {
	case v := <-auth:
		if v != "Bearer fixture-token" {
			t.Fatalf("upstream token=%q", v)
		}
	case <-time.After(time.Second):
		t.Fatal("no upstream handshake")
	}
	bare := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	if c, _, e := bare.Dial(wsURL(srv.URL)+"/?router=1122334455667788", nil); e == nil {
		c.Close()
		t.Fatal("unauthenticated gateway admitted")
	}
	h := http.Header{"Authorization": []string{"Bearer fixture-token"}}
	c2, _, err := bare.Dial(wsURL(srv.URL)+"/?router=0102030405060708", h)
	if err != nil {
		t.Fatal(err)
	}
	c2.Close()
}
