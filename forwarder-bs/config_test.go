package main

import (
	"os"
	"path/filepath"
	"testing"
)

func configArgs() []string {
	return []string{"--connect-uri=ws://127.0.0.1:1234", "--client-id=0011", "--client-key=2233", "--gateway=fixture"}
}

func TestConfigPrecedenceAndMinimum(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte("batch-sec=30\nlog-level=warn\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := append(configArgs(), "--config="+p, "--batch-sec=10", "--max-messages=1", "--max-batches=1", "--max-buffer-bytes=1024", "--shutdown-deadline-ms=100")
	c, err := ParseForwarderConfig(args, []string{"BATCH_SEC=20", "LOG_LEVEL=debug"})
	if err != nil {
		t.Fatal(err)
	}
	if c.FlushInterval != 10 || c.LogLevel != "debug" || c.MaxMessages != 1 || c.MaxBatches != 1 || c.MaxBufferBytes != 1024 {
		t.Fatalf("incorrect precedence/resource profile: %+v", c)
	}
	if c.CertsDir != "" || c.ListenTLSCert != "" {
		t.Fatal("default configuration creates TLS or credential storage")
	}
}

func TestConfigRejectsInvalidAndConflictingLimits(t *testing.T) {
	for _, arg := range []string{"--max-messages=0", "--max-buffer-bytes=1023", "--max-batches=-1", "--max-messages=999999999999999", "--unknown=x", "--request-timeout=0", "--shutdown-deadline-ms=-1"} {
		if _, err := ParseForwarderConfig(append(configArgs(), arg), nil); err == nil {
			t.Errorf("accepted %s", arg)
		}
	}
	if _, err := ParseForwarderConfig(append(configArgs(), "--batch-sec=10", "--flush-interval=20"), nil); err == nil {
		t.Fatal("accepted conflicting aliases")
	}
	if _, err := ParseForwarderConfig(configArgs(), []string{"BATCH_SEC=10", "FLUSH_INTERVAL=20"}); err == nil {
		t.Fatal("accepted conflicting env aliases")
	}
}
