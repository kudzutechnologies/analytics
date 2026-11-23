package forwarderbs

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

func captureConnectionLogs(t *testing.T) *logtest.Hook {
	t.Helper()
	logger := log.StandardLogger()
	old := logger.ReplaceHooks(make(log.LevelHooks))
	hook := logtest.NewGlobal()
	t.Cleanup(func() { logger.ReplaceHooks(old) })
	return hook
}

func connectionLogMessages(hook *logtest.Hook) string {
	var messages []string
	for _, entry := range hook.AllEntries() {
		messages = append(messages, entry.Message)
	}
	return strings.Join(messages, "\n")
}

func TestCUPSDiagnosticsExposeEmptyBootstrapWithoutLoggingPayload(t *testing.T) {
	hook := captureConnectionLogs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(make([]byte, CupsNullResponseLen))
	}))
	defer upstream.Close()
	_, server, store := managedCUPSFixture(t, upstream.URL)
	body := pollCUPS(t, server, cupsRequest{Router: json.RawMessage(`"0102030405060708"`), Station: "request-payload-secret"})
	parsed, err := ParseCUPSUpdateResponseFull(body)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.TcURI == "" || len(parsed.TcCred) == 0 {
		t.Fatal("diagnostics changed local credential provisioning")
	}
	if _, err := store.upstreamCredential("0102030405060708_tc"); err == nil {
		t.Fatal("empty upstream response fabricated upstream credentials")
	}
	logs := connectionLogMessages(hook)
	for _, want := range []string{"CUPS upstream response status=200 bytes=14", "tc_credential_bytes=0", "CUPS upstream returned no updates with zero local upstream credential CRCs", "CUPS response sent"} {
		if !strings.Contains(logs, want) {
			t.Errorf("missing diagnostic %q", want)
		}
	}
	if strings.Contains(logs, "request-payload-secret") {
		t.Fatal("logged request body")
	}
}

func TestCUPSDiagnosticsExposeRejectionWithoutLoggingUpstreamBody(t *testing.T) {
	hook := captureConnectionLogs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream-response-secret", http.StatusForbidden)
	}))
	defer upstream.Close()
	_, server, _ := managedCUPSFixture(t, upstream.URL)
	response, err := server.Client().Post(server.URL+"/update-info", "application/json", bytes.NewBufferString(`{"router":"0102030405060708"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d", response.StatusCode)
	}
	logs := connectionLogMessages(hook)
	if !strings.Contains(logs, "CUPS failed status=403 reason=upstream CUPS request rejected") {
		t.Fatal("upstream rejection was not logged")
	}
	if strings.Contains(logs, "upstream-response-secret") {
		t.Fatal("logged upstream response body")
	}
}

func TestConnectionDiagnosticsRedactEndpointsAndErrors(t *testing.T) {
	if got := upstreamLogOrigin("wss://user:password@example.test:443/private-token?token=query-secret#secret"); got != "wss://example.test:443" {
		t.Fatalf("origin=%q", got)
	}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&url.Error{Op: "Post", URL: "https://example.test?token=secret", Err: x509.UnknownAuthorityError{}}, "untrusted upstream certificate"},
		{&upstreamHandshakeError{status: 403, err: errors.New("response-secret")}, "HTTP 403"},
		{errors.New("header-value-secret"), "upstream connection failed (network, TLS, or routing)"},
	} {
		if got := upstreamFailureReason(tc.err); got != tc.want {
			t.Fatalf("reason=%q want=%q", got, tc.want)
		}
	}
}
