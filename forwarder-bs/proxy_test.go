package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func startProxyFixture(t *testing.T, c *WSProxyConfig) (*WSProxy, *httptest.Server) {
	t.Helper()
	s := httptest.NewUnstartedServer(nil)
	c.PublicURI = wsURL("http://"+s.Listener.Addr().String()) + "/"
	if c.MaxStreams == 0 {
		c.MaxStreams = 4
	}
	p, e := NewWSProxy(c)
	if e != nil {
		t.Fatal(e)
	}
	s.Config.Handler = p.server.Handler
	s.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		p.Shutdown(ctx)
		s.Close()
	})
	return p, s
}

func TestDiscoveryUsesAllocatedEndpointWithoutDisk(t *testing.T) {
	up := websocket.Upgrader{}
	hits := make(chan string, 1)
	var lns *httptest.Server
	lns = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := up.Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		if r.URL.Path == "/router-info" {
			var req RouterInfoRequest
			if c.ReadJSON(&req) != nil {
				return
			}
			c.WriteJSON(RouterInfoResponse{Router: gatewayIDToEUIFormat(req.Router), Muxs: "::1", URI: wsURL(lns.URL) + "/assigned"})
			return
		}
		hits <- r.URL.RequestURI()
		for {
			mt, msg, e := c.ReadMessage()
			if e != nil {
				return
			}
			if c.WriteMessage(mt, msg) != nil {
				return
			}
		}
	}))
	defer lns.Close()
	_, proxy := startProxyFixture(t, &WSProxyConfig{ConnectURI: wsURL(lns.URL)})
	discovery, _, e := websocket.DefaultDialer.Dial(wsURL(proxy.URL)+"/router-info", nil)
	if e != nil {
		t.Fatal(e)
	}
	defer discovery.Close()
	discovery.WriteJSON(map[string]string{"router": "f::1"})
	var response RouterInfoResponse
	if e = discovery.ReadJSON(&response); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(response.URI, "router=000f000000000001") {
		t.Fatalf("lost identity: %s", response.URI)
	}
	data, _, e := websocket.DefaultDialer.Dial(response.URI, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer data.Close()
	select {
	case path := <-hits:
		if !strings.HasPrefix(path, "/assigned?") {
			t.Fatalf("wrong LNS route: %s", path)
		}
	case <-time.After(time.Second):
		t.Fatal("no allocated connection")
	}
	raw := []byte("{\"msgtype\":\"version\", \"features\":\"fixture\"}")
	if e = data.WriteMessage(websocket.TextMessage, raw); e != nil {
		t.Fatal(e)
	}
	_, got, e := data.ReadMessage()
	if e != nil || string(got) != string(raw) {
		t.Fatalf("payload mutated: %q %v", got, e)
	}
}

func TestKeepaliveAndReplacement(t *testing.T) {
	up := websocket.Upgrader{}
	lns := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := up.Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		for {
			mt, msg, e := c.ReadMessage()
			if e != nil {
				return
			}
			if c.WriteMessage(mt, msg) != nil {
				return
			}
		}
	}))
	defer lns.Close()
	p, proxy := startProxyFixture(t, &WSProxyConfig{ConnectURI: wsURL(lns.URL), PingInterval: 20 * time.Millisecond, PongTimeout: 80 * time.Millisecond})
	url := wsURL(proxy.URL) + "/?router=0102030405060708"
	first, _, e := websocket.DefaultDialer.Dial(url, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer first.Close()
	// A reader is required for the client to answer WebSocket ping frames.
	firstDone := make(chan error, 1)
	go func() { _, _, err := first.ReadMessage(); firstDone <- err }()
	select {
	case err := <-firstDone:
		t.Fatalf("healthy idle gateway disconnected: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
	old, _ := p.sessions.Get("0102030405060708")
	second, _, e := websocket.DefaultDialer.Dial(url, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	deadline := time.Now().Add(time.Second)
	var replacement *session
	for time.Now().Before(deadline) {
		replacement, _ = p.sessions.Get("0102030405060708")
		if replacement != nil && replacement != old {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if replacement == nil || replacement == old {
		t.Fatal("replacement not installed")
	}
	first.Close()
	second.WriteMessage(websocket.TextMessage, []byte("still connected"))
	second.SetReadDeadline(time.Now().Add(time.Second))
	_, got, e := second.ReadMessage()
	if e != nil || string(got) != "still connected" {
		t.Fatalf("old cleanup killed replacement: %q %v", got, e)
	}
}

func TestUnauthenticatedOriginRedirectRejected(t *testing.T) {
	c := WSProxyConfig{ConnectURI: "wss://configured.example:443"}
	if c.allowedUpstream("wss://attacker.example:443") {
		t.Fatal("accepted foreign origin")
	}
	if c.allowedUpstream("wss://user:secret@configured.example:443") {
		t.Fatal("accepted URI credentials")
	}
}
