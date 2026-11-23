package forwarderbs

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
)

// Compare before writing: unchanged polls must not cause flash wear. Gateway
// records are replaced atomically, keeping credentials and endpoint state together.
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
	r, e := s.Persistence.Load(s.storageContext(), strings.TrimSuffix(id, "_tc"))
	if e != nil {
		return nil, e
	}
	b := r.CUPS
	if strings.HasSuffix(id, "_tc") {
		b = r.TC
	}
	if len(b) == 0 {
		return nil, ErrNotFound
	}
	return b, nil
}

func (s *CertStore) putUpstreamCredential(id string, blob []byte) error {
	if _, _, _, _, e := decodeCredential(blob); e != nil {
		return e
	}
	p := GatewayRecordPatch{CUPS: &blob}
	if strings.HasSuffix(id, "_tc") {
		p = GatewayRecordPatch{TC: &blob}
	}
	return patchRecord(s.storageContext(), s.Persistence, strings.TrimSuffix(id, "_tc"), p)
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
	CupsURI string `json:"cups_uri" bson:"cups_uri"`
	TcURI   string `json:"tc_uri" bson:"tc_uri"`
}

func (s *CertStore) cupsState(id string) upstreamCUPSState {
	r, e := s.Persistence.Load(s.storageContext(), id)
	if e != nil {
		return upstreamCUPSState{}
	}
	return r.State
}

func (s *CertStore) putCUPSState(id string, state upstreamCUPSState) error {
	return patchRecord(s.storageContext(), s.Persistence, id, GatewayRecordPatch{State: &state})
}
