package forwarderbs

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Peers may send useful traffic or their own pings without answering ours.
// Receiving those frames is evidence of activity, even without a pong.
func TestKeepaliveCountsIncomingApplicationActivity(t *testing.T) {
	for _, side := range []string{"gateway", "upstream"} {
		t.Run(side, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				if side == "upstream" {
					conn.SetPingHandler(func(string) error { return nil })
				}
				for {
					kind, raw, err := conn.ReadMessage()
					if err != nil {
						return
					}
					if err = conn.WriteMessage(kind, raw); err != nil {
						return
					}
				}
			}))
			defer upstream.Close()
			_, proxy := startProxyFixture(t, &WSProxyConfig{ConnectURI: wsURL(upstream.URL), PingInterval: 20 * time.Millisecond, PongTimeout: 150 * time.Millisecond})
			conn, _, err := websocket.DefaultDialer.Dial(wsURL(proxy.URL)+"/?router=0102030405060708", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if side == "gateway" {
				conn.SetPingHandler(func(string) error { return nil })
			}
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			for i := 0; i < 10; i++ {
				time.Sleep(35 * time.Millisecond)
				want := fmt.Sprintf("active-%d", i)
				if err = conn.WriteMessage(websocket.TextMessage, []byte(want)); err != nil {
					t.Fatal(err)
				}
				_, got, err := conn.ReadMessage()
				if err != nil || string(got) != want {
					t.Fatalf("%s peer timed out while active: got=%q err=%v", side, got, err)
				}
			}
		})
	}
}

func TestKeepaliveCountsIncomingPingActivity(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetPingHandler(func(string) error { return nil })
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(25 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					if conn.WriteControl(websocket.PingMessage, []byte("keepalive"), time.Now().Add(time.Second)) != nil {
						return
					}
				}
			}
		}()
		for {
			kind, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err = conn.WriteMessage(kind, raw); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	_, proxy := startProxyFixture(t, &WSProxyConfig{ConnectURI: wsURL(upstream.URL), PingInterval: 20 * time.Millisecond, PongTimeout: 150 * time.Millisecond})
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(proxy.URL)+"/?router=0102030405060708", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	result := make(chan error, 1)
	go func() { _, _, err := conn.ReadMessage(); result <- err }()
	select {
	case err := <-result:
		t.Fatalf("upstream sending pings was disconnected: %v", err)
	case <-time.After(400 * time.Millisecond):
	}
	if err = conn.WriteMessage(websocket.TextMessage, []byte("after-pings")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("no response after ping-only activity")
	}
}

func TestKeepaliveStillTimesOutSilentUpstream(t *testing.T) {
	hook := captureConnectionLogs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetPingHandler(func(string) error { return nil })
		for {
			if _, _, err = conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	_, proxy := startProxyFixture(t, &WSProxyConfig{ConnectURI: wsURL(upstream.URL), PingInterval: 20 * time.Millisecond, PongTimeout: 150 * time.Millisecond})
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(proxy.URL)+"/?router=0102030405060708", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err = conn.ReadMessage(); err == nil {
		t.Fatal("silent upstream was not disconnected")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(connectionLogMessages(hook), "upstream WebSocket read ended reason=read_timeout") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timeout did not identify the upstream side")
}

func TestReconnectForwardsNewVersionAndRouterConfig(t *testing.T) {
	hook := captureConnectionLogs(t)
	var connections int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		number := atomic.AddInt32(&connections, 1)
		kind, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if string(raw) != `{"msgtype":"version"}` {
			t.Error("missing fresh version on reconnect")
			return
		}
		if err = conn.WriteMessage(kind, []byte(`{"msgtype":"router_config","DRs":[]}`)); err != nil {
			return
		}
		if number == 1 {
			conn.UnderlyingConn().Close()
			return
		}
		for {
			kind, raw, err = conn.ReadMessage()
			if err != nil {
				return
			}
			if err = conn.WriteMessage(kind, raw); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	p, proxy := startProxyFixture(t, &WSProxyConfig{ConnectURI: wsURL(upstream.URL), PingInterval: 20 * time.Millisecond, PongTimeout: 150 * time.Millisecond})
	connect := func() *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial(wsURL(proxy.URL)+"/?router=0102030405060708", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err = conn.WriteMessage(websocket.TextMessage, []byte(`{"msgtype":"version"}`)); err != nil {
			t.Fatal(err)
		}
		_, raw, err := conn.ReadMessage()
		if err != nil || !strings.Contains(string(raw), `"router_config"`) {
			t.Fatalf("router configuration not forwarded: %q %v", raw, err)
		}
		return conn
	}
	first := connect()
	if _, _, err := first.ReadMessage(); err == nil {
		t.Fatal("first abrupt close was not propagated")
	}
	second := connect()
	result := make(chan error, 1)
	go func() {
		_, raw, err := second.ReadMessage()
		if err == nil && string(raw) != "after-reconnect" {
			err = fmt.Errorf("unexpected echo")
		}
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("reconnected session failed: %v", err)
	case <-time.After(400 * time.Millisecond):
	}
	if err := second.WriteMessage(websocket.TextMessage, []byte("after-reconnect")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("no forwarded traffic after reconnect")
	}
	p.logSessionStatus()
	logs := connectionLogMessages(hook)
	if strings.Count(logs, "LNS version forwarded") != 2 || strings.Count(logs, "LNS router_config forwarded") != 2 {
		t.Fatal("missing per-session application handshake diagnostics")
	}
	if !strings.Contains(logs, "version_forwarded=true router_config_forwarded=true") {
		t.Fatal("missing application handshake status")
	}
}
