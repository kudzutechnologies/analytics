package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"hash/crc32"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func managedCUPSFixture(t *testing.T, upstream string) (*WSProxy, *httptest.Server, *CertStore) {
	t.Helper()
	store, e := NewCertStore(t.TempDir(), "", "")
	if e != nil {
		t.Fatal(e)
	}
	s := httptest.NewUnstartedServer(nil)
	host, _, _ := net.SplitHostPort(s.Listener.Addr().String())
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1234), Subject: pkix.Name{CommonName: host}, IPAddresses: []net.IP{net.ParseIP(host)}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, template, store.caCert, &key.PublicKey, store.caKey)
	if e != nil {
		t.Fatal(e)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	certPath, keyPath := filepath.Join(store.dir, "listen.crt"), filepath.Join(store.dir, "listen.key")
	if e = writePEM(certPath, "CERTIFICATE", der); e != nil {
		t.Fatal(e)
	}
	if e = writePEM(keyPath, "EC PRIVATE KEY", keyDER); e != nil {
		t.Fatal(e)
	}
	p, e := NewWSProxy(&WSProxyConfig{ConnectURI: wsURL(upstream), MaxStreams: 2, PublicURI: "wss://" + s.Listener.Addr().String() + "/", CUPSMode: "managed", CertStore: store, ListenTLSCert: certPath, ListenTLSKey: keyPath})
	if e != nil {
		t.Fatal(e)
	}
	s.Config.Handler = p.server.Handler
	pair, e := tls.X509KeyPair(pemCertificate(der), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	if e != nil {
		t.Fatal(e)
	}
	s.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	s.StartTLS()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		p.Shutdown(ctx)
		s.Close()
	})
	return p, s, store
}

func pollCUPS(t *testing.T, s *httptest.Server, request cupsRequest) []byte {
	t.Helper()
	raw, _ := json.Marshal(request)
	r, e := s.Client().Post(s.URL+"/update-info", "application/json", bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		t.Fatal(e)
	}
	if r.StatusCode != 200 {
		t.Fatalf("CUPS %d: %s", r.StatusCode, b)
	}
	return b
}

func TestCUPSPollIsIdempotentAndStoresPurposeTokens(t *testing.T) {
	var root []byte
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req cupsRequest
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if calls == 1 {
			cups := append(append(append([]byte{}, root...), 0, 0, 0, 0), []byte("Bearer cups-token")...)
			tc := append(append(append([]byte{}, root...), 0, 0, 0, 0), []byte("Bearer tc-token")...)
			body, e := BuildCupsResponseWithCreds("", "", cups, tc)
			if e != nil {
				t.Error(e)
			}
			w.Write(body)
			return
		}
		if r.Header.Get("Authorization") != "Bearer cups-token" {
			t.Errorf("CUPS got wrong token: %q", r.Header.Get("Authorization"))
		}
		if req.CupsCRC == nil || req.TcCRC == nil || *req.CupsCRC == 0 || *req.TcCRC == 0 || *req.CupsCRC == *req.TcCRC {
			t.Error("upstream CRCs not purpose-specific")
		}
		w.Write(make([]byte, CupsNullResponseLen))
	}))
	defer upstream.Close()
	p, s, store := managedCUPSFixture(t, upstream.URL)
	root = store.GetCACertDER()
	router := json.RawMessage(`"0102030405060708"`)
	first := pollCUPS(t, s, cupsRequest{Router: router, Keys: []uint32{}})
	parsed, e := ParseCUPSUpdateResponseFull(first)
	if e != nil {
		t.Fatal(e)
	}
	if parsed.CupsURI == "" || parsed.TcURI == "" || len(parsed.CupsCred) == 0 || len(parsed.TcCred) == 0 {
		t.Fatal("initial proxy provisioning missing")
	}
	before, e := os.Stat(filepath.Join(store.dir, "0102030405060708.crt"))
	if e != nil {
		t.Fatal(e)
	}
	crc := crc32.ChecksumIEEE(parsed.CupsCred)
	request := cupsRequest{Router: router, CupsURI: parsed.CupsURI, TcURI: &parsed.TcURI, CupsCRC: &crc, TcCRC: &crc, Keys: []uint32{}}
	for i := 0; i < 3; i++ {
		body := pollCUPS(t, s, request)
		if !IsCupsNullResponse(body) {
			t.Fatalf("unchanged poll produced %d-byte update", len(body))
		}
	}
	after, _ := os.Stat(filepath.Join(store.dir, "0102030405060708.crt"))
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged poll rewrote client certificate")
	}
	if got := p.upstreamHeaders("0102030405060708", "tc", nil).Get("Authorization"); got != "Bearer tc-token" {
		t.Fatalf("TC got wrong token: %q", got)
	}
}

func TestCUPSStreamsSignedFirmware(t *testing.T) {
	const size = 2 << 20
	trailer := make([]byte, 4)
	binary.LittleEndian.PutUint32(trailer, 8)
	trailer = append(trailer, 1, 2, 3, 4, 5, 6, 7, 8)
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, size)
	trailer = append(trailer, word...)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix, _ := BuildCupsResponseFull("", "", nil, nil, trailer)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(prefix)+size))
		w.Write(prefix)
		var block [4096]byte
		for i := 0; i < size/len(block); i++ {
			w.Write(block[:])
		}
	}))
	defer upstream.Close()
	_, s, _ := managedCUPSFixture(t, upstream.URL)
	body := pollCUPS(t, s, cupsRequest{Router: json.RawMessage(`"0102030405060708"`)})
	parsed, e := ParseCUPSUpdateResponseFull(body)
	if e != nil {
		t.Fatal(e)
	}
	if len(parsed.Trailing) != len(trailer)+size || !bytes.Equal(parsed.Trailing[:len(trailer)], trailer) {
		t.Fatal("firmware trailer changed")
	}
}

func TestCUPSRejectsMalformedLengths(t *testing.T) {
	body, e := BuildCupsResponseWithCreds("", "", nil, nil)
	if e != nil || !IsCupsNullResponse(body) {
		t.Fatalf("null response: %x %v", body, e)
	}
	for _, bad := range [][]byte{{0x30, 0x80}, {0x30, 0xff, 1, 2, 3, 4, 5, 6}, {0x30, 0x84, 0xff, 0xff, 0xff, 0xff}} {
		if _, e := derObjectLength(bad); e == nil {
			t.Errorf("accepted DER %x", bad)
		}
	}
	if _, e := BuildCupsResponseWithCreds("", "", make([]byte, 65536), nil); e == nil {
		t.Fatal("credential uint16 overflow")
	}
	for i := 0; i < len(body); i++ {
		if _, e := ParseCUPSUpdateResponseFull(body[:i]); e == nil {
			t.Fatalf("accepted truncation %d", i)
		}
	}
	unknown := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "unknown length")
	}))
	defer unknown.Close()
	_, s, _ := managedCUPSFixture(t, unknown.URL)
	raw := []byte(`{"router":"0102030405060708"}`)
	r, e := s.Client().Post(s.URL+"/update-info", "application/json", bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 502 {
		t.Fatal("accepted unknown-length firmware response")
	}
}
