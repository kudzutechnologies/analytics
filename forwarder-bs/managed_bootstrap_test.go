package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestManagedCUPSBootstrapReportsOnlyActualTCURI(t *testing.T) {
	for _, tc := range []struct{ name, assigned, explicit, want string }{
		{name: "fresh"},
		{name: "assigned", assigned: "wss://lns.example.test/assigned", want: "wss://lns.example.test/assigned"},
		{name: "explicit", explicit: "wss://lns.example.test/configured", want: "wss://lns.example.test/configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan cupsRequest, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request cupsRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				requests <- request
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Write(make([]byte, CupsNullResponseLen))
			}))
			defer upstream.Close()
			p, server, store := managedCUPSFixture(t, upstream.URL)
			p.config.ConnectDataURI = tc.explicit
			if tc.assigned != "" {
				if err := store.putCUPSState("24e124fffef37b09", upstreamCUPSState{TcURI: tc.assigned}); err != nil {
					t.Fatal(err)
				}
			}
			// The gateway's local proxy URI and CRCs must not masquerade as upstream state.
			localTC := wsURL(server.URL)
			crc := uint32(1234)
			pollCUPS(t, server, cupsRequest{Router: json.RawMessage(`"24e124fffef37b09"`), TcURI: &localTC, CupsCRC: &crc, TcCRC: &crc})
			request := <-requests
			if request.TcURI == nil || *request.TcURI != tc.want {
				t.Fatalf("upstream tcUri=%v want=%q", request.TcURI, tc.want)
			}
			if request.CupsCRC == nil || *request.CupsCRC != 0 || request.TcCRC == nil || *request.TcCRC != 0 {
				t.Fatal("reported local proxy credentials as upstream credentials")
			}
		})
	}
}

func managedGatewayDialer(server *httptest.Server) *websocket.Dialer {
	return &websocket.Dialer{TLSClientConfig: server.Client().Transport.(*http.Transport).TLSClientConfig}
}

func TestManagedLNSWaitsForProvisioningAndUsesAssignedEndpoint(t *testing.T) {
	cupsHits := make(chan string, 4)
	cups := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cupsHits <- r.URL.Path
		http.NotFound(w, r)
	}))
	defer cups.Close()
	lnsHits := make(chan string, 4)
	lns := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lnsHits <- r.URL.Path
		if r.URL.Path == "/router-info" {
			http.NotFound(w, r)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		kind, message, err := conn.ReadMessage()
		if err == nil {
			conn.WriteMessage(kind, message)
		}
	}))
	defer lns.Close()
	p, server, store := managedCUPSFixture(t, cups.URL)
	p.config.UpstreamAllowedOrigins = wsURL(lns.URL)
	dialer := managedGatewayDialer(server)
	discover := func() RouterInfoResponse {
		t.Helper()
		conn, _, err := dialer.Dial(wsURL(server.URL)+"/router-info", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err = conn.WriteJSON(map[string]string{"router": "24e124fffef37b09"}); err != nil {
			t.Fatal(err)
		}
		var info RouterInfoResponse
		if err = conn.ReadJSON(&info); err != nil {
			t.Fatal(err)
		}
		return info
	}
	if info := discover(); info.Error != errLNSProvisioningPending.Error() || info.URI != "" {
		t.Fatalf("unexpected unprovisioned response: %+v", info)
	}
	conn, response, err := dialer.Dial(wsURL(server.URL)+"/?router=24e124fffef37b09", nil)
	if conn != nil {
		conn.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unprovisioned data connection: response=%v error=%v", response, err)
	}
	response.Body.Close()
	select {
	case path := <-cupsHits:
		t.Fatalf("used CUPS as LNS: %s", path)
	default:
	}
	// An upstream assignment enables discovery, then 404 fallback to that LNS.
	if err := store.putCUPSState("24e124fffef37b09", upstreamCUPSState{TcURI: wsURL(lns.URL) + "/assigned"}); err != nil {
		t.Fatal(err)
	}
	info := discover()
	if info.Error != "" || !strings.HasPrefix(info.URI, wsURL(server.URL)) {
		t.Fatalf("discovery response: %+v", info)
	}
	conn, _, err = dialer.Dial(info.URI, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err = conn.WriteMessage(websocket.TextMessage, []byte("assigned-route")); err != nil {
		t.Fatal(err)
	}
	_, message, err := conn.ReadMessage()
	if err != nil || string(message) != "assigned-route" {
		t.Fatalf("assigned LNS data: %q %v", message, err)
	}
	for _, want := range []string{"/router-info", "/assigned"} {
		if got := <-lnsHits; got != want {
			t.Fatalf("LNS path=%s want=%s", got, want)
		}
	}
	select {
	case path := <-cupsHits:
		t.Fatalf("used CUPS as LNS after assignment: %s", path)
	default:
	}
}

func TestManagedDiscovery404RequiresKnownDataEndpoint(t *testing.T) {
	discovery := httptest.NewServer(http.NotFoundHandler())
	defer discovery.Close()
	p, server, _ := managedCUPSFixture(t, discovery.URL)
	p.config.ConnectDiscoveryURI = wsURL(discovery.URL) + "/router-info"
	conn, _, err := managedGatewayDialer(server).Dial(wsURL(server.URL)+"/router-info", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.WriteJSON(map[string]string{"router": "24e124fffef37b09"})
	var info RouterInfoResponse
	if err = conn.ReadJSON(&info); err != nil {
		t.Fatal(err)
	}
	if info.Error != errLNSProvisioningPending.Error() || info.URI != "" {
		t.Fatalf("fabricated data endpoint: %+v", info)
	}
}
