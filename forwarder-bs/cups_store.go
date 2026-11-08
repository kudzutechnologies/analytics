package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Compare before writing: unchanged polls must not cause flash wear. A complete
// credential blob is one atomically replaced file, so cert/key cannot diverge.
func atomicCredentialWrite(path string, data []byte) error {
	if st, err := os.Stat(path); err == nil && st.Size() == int64(len(data)) {
		if old, e := os.ReadFile(path); e == nil && bytes.Equal(old, data) {
			return nil
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".credential-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
func decodeCredential(blob []byte) (trust, cert, key []byte, token string, err error) {
	if len(blob) < 4 {
		err = fmt.Errorf("credential too short")
		return
	}
	n, e := derObjectLength(blob)
	if e != nil {
		err = e
		return
	}
	trust = blob[:n]
	if _, e = x509.ParseCertificate(trust); e != nil {
		err = e
		return
	}
	rest := blob[n:]
	if len(rest) >= 4 && bytes.Equal(rest[:4], make([]byte, 4)) {
		rest = rest[4:]
		if len(rest) == 0 || bytes.Equal(rest, make([]byte, 4)) {
			return
		}
		token = string(rest)
		if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n\x00") {
			err = fmt.Errorf("invalid upstream token")
		}
		return
	}
	n, e = derObjectLength(rest)
	if e != nil {
		err = e
		return
	}
	cert = rest[:n]
	rest = rest[n:]
	n, e = derObjectLength(rest)
	if e != nil || n != len(rest) {
		err = fmt.Errorf("invalid private key segment")
		return
	}
	key = rest
	_, e = tls.X509KeyPair(pemCertificate(cert), pemEncodeKey(key))
	if e != nil {
		err = e
	}
	return
}
func pemCertificate(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
func (s *CertStore) upstreamCredential(id string) ([]byte, error) {
	base := strings.TrimSuffix(id, "_tc")
	if _, err := ParseGatewayID(base); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(s.dir, id+".server.cred"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 65536))
	if err == nil && len(b) > 65535 {
		err = fmt.Errorf("credential too large")
	}
	return b, err
}
func (s *CertStore) putUpstreamCredential(id string, blob []byte) error {
	if !s.writable {
		return fmt.Errorf("credential storage is read-only")
	}
	base := strings.TrimSuffix(id, "_tc")
	if _, err := ParseGatewayID(base); err != nil {
		return err
	}
	if _, _, _, _, err := decodeCredential(blob); err != nil {
		return err
	}
	return atomicCredentialWrite(filepath.Join(s.dir, id+".server.cred"), blob)
}
func (s *CertStore) upstreamToken(id string) string {
	blob, err := s.upstreamCredential(id)
	if err != nil {
		return ""
	}
	_, _, _, token, err := decodeCredential(blob)
	if err != nil || token == "" {
		return ""
	}
	if !strings.Contains(token, " ") {
		token = "Bearer " + token
	}
	return token
}
func (s *CertStore) upstreamCRC(id string) uint32 {
	blob, err := s.upstreamCredential(id)
	if err != nil {
		return 0
	}
	trust, cert, key, token, err := decodeCredential(blob)
	if err != nil {
		return 0
	}
	crc := crc32.ChecksumIEEE(trust)
	if len(cert) == 0 {
		cert = make([]byte, 4)
	}
	crc = crc32.Update(crc, crc32.IEEETable, cert)
	if token != "" {
		key = []byte(token)
	}
	if len(key) == 0 {
		key = make([]byte, 4)
	}
	return crc32.Update(crc, crc32.IEEETable, key)
}

type upstreamCUPSState struct {
	CupsURI string `json:"cups_uri"`
	TcURI   string `json:"tc_uri"`
}

func (s *CertStore) cupsState(id string) upstreamCUPSState {
	var state upstreamCUPSState
	f, err := os.Open(filepath.Join(s.dir, id+".cups-state.json"))
	if err != nil {
		return state
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err == nil && len(raw) <= 4096 {
		_ = json.Unmarshal(raw, &state)
	}
	return state
}
func (s *CertStore) putCUPSState(id string, state upstreamCUPSState) error {
	if !s.writable {
		return fmt.Errorf("credential storage is read-only")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicCredentialWrite(filepath.Join(s.dir, id+".cups-state.json"), raw)
}
