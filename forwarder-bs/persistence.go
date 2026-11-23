package forwarderbs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var ErrNotFound = errors.New("gateway record not found")
var ErrReadOnly = errors.New("gateway persistence is read-only")
var ErrConflict = errors.New("gateway revision conflict")

type CertificatePEM struct {
	Cert []byte `json:"cert" bson:"cert"`
	Key  []byte `json:"key" bson:"key"`
}
type GatewayRecord struct {
	LNSConfig  *GatewayLNSConfig `json:"lns_config,omitempty" bson:"lns_config,omitempty"`
	DataURI    string            `json:"data_uri,omitempty" bson:"data_uri,omitempty"`
	LegacyCUPS CertificatePEM    `json:"legacy_cups" bson:"legacy_cups"`
	LegacyTC   CertificatePEM    `json:"legacy_tc" bson:"legacy_tc"`
	EUI        string            `json:"eui" bson:"eui"`
	Revision   uint64            `json:"revision" bson:"revision"`
	Client     CertificatePEM    `json:"client" bson:"client"`
	CUPS       []byte            `json:"cups" bson:"cups"`
	TC         []byte            `json:"tc" bson:"tc"`
	State      CUPSState         `json:"state" bson:"state"`
}
type GatewayRecordPatch struct {
	LegacyCUPS *CertificatePEM   `json:"legacy_cups,omitempty" bson:"legacy_cups,omitempty"`
	LegacyTC   *CertificatePEM   `json:"legacy_tc,omitempty" bson:"legacy_tc,omitempty"`
	LNSConfig  *GatewayLNSConfig `json:"lns_config,omitempty" bson:"lns_config,omitempty"`
	DataURI    *string           `json:"data_uri,omitempty" bson:"data_uri,omitempty"`
	Client     *CertificatePEM   `json:"client,omitempty" bson:"client,omitempty"`
	CUPS       *[]byte           `json:"cups,omitempty" bson:"cups,omitempty"`
	TC         *[]byte           `json:"tc,omitempty" bson:"tc,omitempty"`
	State      *CUPSState        `json:"state,omitempty" bson:"state,omitempty"`
}
type GatewayPersistence interface {
	Load(context.Context, string) (GatewayRecord, error)
	Save(context.Context, GatewayRecord, uint64) (uint64, error)
}
type GatewayPatcher interface {
	Patch(context.Context, string, GatewayRecordPatch, uint64) (uint64, error)
}
type FilesystemPersistence struct {
	Dir      string
	ReadOnly bool
	mu       sync.Mutex
}

func (s *FilesystemPersistence) load(id string) (GatewayRecord, error) {
	r := GatewayRecord{EUI: id}
	if _, e := ParseGatewayID(id); e != nil {
		return r, e
	}
	b, e := os.ReadFile(filepath.Join(s.Dir, id+".gateway.json"))
	if e == nil {
		e = json.Unmarshal(b, &r)
		if e == nil && r.EUI != id {
			e = errors.New("gateway record identity mismatch")
		}
		return r, e
	}
	if !os.IsNotExist(e) {
		return r, e
	}
	found := false
	var readErr error
	read := func(suffix string) []byte {
		b, e := os.ReadFile(filepath.Join(s.Dir, id+suffix))
		if e != nil && !os.IsNotExist(e) {
			readErr = e
		}
		if e == nil {
			found = true
		}
		return b
	}
	r.LegacyCUPS = CertificatePEM{read(".server.crt"), read(".server.key")}
	r.LegacyTC = CertificatePEM{read("_tc.server.crt"), read("_tc.server.key")}
	r.DataURI = string(read(".data_uri"))
	if b := read(".lns_config"); len(b) > 0 {
		if e = json.Unmarshal(b, &r.LNSConfig); e != nil {
			return r, e
		}
	}
	r.Client = CertificatePEM{read(".crt"), read(".key")}
	r.CUPS = read(".server.cred")
	r.TC = read("_tc.server.cred")
	b = read(".cups-state.json")
	if len(b) > 0 {
		if e = json.Unmarshal(b, &r.State); e != nil {
			return r, e
		}
	}
	if readErr != nil {
		return r, readErr
	}
	if !found {
		return r, ErrNotFound
	}
	return r, nil
}
func (s *FilesystemPersistence) Load(ctx context.Context, id string) (GatewayRecord, error) {
	if e := ctx.Err(); e != nil {
		return GatewayRecord{}, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id)
}
func (s *FilesystemPersistence) save(r GatewayRecord, rev uint64) (uint64, error) {
	if s.ReadOnly {
		return 0, ErrReadOnly
	}
	old, e := s.load(r.EUI)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return 0, e
	}
	if old.Revision != rev {
		return 0, ErrConflict
	}
	r.Revision = rev
	before, _ := json.Marshal(old)
	after, _ := json.Marshal(r)
	if e == nil && old.Revision > 0 && string(before) == string(after) {
		return rev, nil
	}
	r.Revision = rev + 1
	b, e := json.Marshal(r)
	if e != nil {
		return 0, e
	}
	if e = os.MkdirAll(s.Dir, 0750); e != nil {
		return 0, e
	}
	e = atomicCredentialWrite(filepath.Join(s.Dir, r.EUI+".gateway.json"), b)
	return r.Revision, e
}
func (s *FilesystemPersistence) Save(ctx context.Context, r GatewayRecord, rev uint64) (uint64, error) {
	if e := ctx.Err(); e != nil {
		return 0, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(r, rev)
}
func (s *FilesystemPersistence) Patch(ctx context.Context, id string, p GatewayRecordPatch, rev uint64) (uint64, error) {
	if e := ctx.Err(); e != nil {
		return 0, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, e := s.load(id)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return 0, e
	}
	applyPatch(&r, p)
	return s.save(r, rev)
}
func applyPatch(r *GatewayRecord, p GatewayRecordPatch) {
	if p.LegacyCUPS != nil {
		r.LegacyCUPS = *p.LegacyCUPS
	}
	if p.LegacyTC != nil {
		r.LegacyTC = *p.LegacyTC
	}
	if p.LNSConfig != nil {
		r.LNSConfig = p.LNSConfig
	}
	if p.DataURI != nil {
		r.DataURI = *p.DataURI
	}
	if p.Client != nil {
		r.Client = *p.Client
	}
	if p.CUPS != nil {
		r.CUPS = *p.CUPS
	}
	if p.TC != nil {
		r.TC = *p.TC
	}
	if p.State != nil {
		r.State = *p.State
	}
}
func patchRecord(ctx context.Context, s GatewayPersistence, id string, p GatewayRecordPatch) error {
	for i := 0; i < 3; i++ {
		r, e := s.Load(ctx, id)
		if errors.Is(e, ErrNotFound) {
			r = GatewayRecord{EUI: id}
		} else if e != nil {
			return e
		}
		if native, ok := s.(GatewayPatcher); ok {
			_, e = native.Patch(ctx, id, p, r.Revision)
		} else {
			applyPatch(&r, p)
			_, e = s.Save(ctx, r, r.Revision)
		}
		if !errors.Is(e, ErrConflict) {
			return e
		}
	}
	return ErrConflict
}

type CUPSState = upstreamCUPSState
