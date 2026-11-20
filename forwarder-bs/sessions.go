package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"sync"
)

type sessionRegistry struct {
	mu       sync.Mutex
	items    map[string]*session
	capacity int
	closed   bool
}

func (r *sessionRegistry) Add(id string, s *session) bool {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return false
	}
	old := r.items[id]
	if old == nil && len(r.items) >= r.capacity {
		r.mu.Unlock()
		return false
	}
	r.items[id] = s
	r.mu.Unlock()
	if old != nil {
		old.close()
	}
	return true
}
func (r *sessionRegistry) Get(id string) (*session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.items[id]
	return s, ok
}
func (r *sessionRegistry) RemoveCurrent(id string, s *session) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.items[id] != s {
		return false
	}
	delete(r.items, id)
	return true
}
func (r *sessionRegistry) Values() []*session {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*session, 0, len(r.items))
	for _, s := range r.items {
		out = append(out, s)
	}
	return out
}
func (p *WSProxy) admit(w http.ResponseWriter) bool {
	select {
	case <-p.root.Done():
		http.Error(w, "shutting down", 503)
		return false
	default:
	}
	select {
	case p.admission <- struct{}{}:
		return true
	default:
		http.Error(w, "handshake capacity reached", 429)
		return false
	}
}
func (p *WSProxy) release() { <-p.admission }
func (p *WSProxy) track(c *websocket.Conn, add bool) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if add {
		select {
		case <-p.root.Done():
			c.Close()
		default:
			p.pending[c] = struct{}{}
		}
	} else {
		delete(p.pending, c)
	}
}
func (p *WSProxy) setRoute(id, uri string) {
	p.routesMu.Lock()
	defer p.routesMu.Unlock()
	if uri == "" {
		delete(p.routes, id)
		return
	}
	if _, ok := p.routes[id]; !ok && len(p.routes) >= p.config.MaxStreams {
		for k := range p.routes {
			delete(p.routes, k)
			break
		}
	}
	p.routes[id] = uri
}
func (p *WSProxy) publicURI() string {
	if p.config.PublicURI != "" {
		return p.config.PublicURI
	}
	scheme := "ws"
	if p.config.ListenTLSCert != "" {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s/", scheme, net.JoinHostPort(p.config.ListenHost, fmt.Sprint(p.config.ListenPort)))
}

func (r *sessionRegistry) CloseAll() {
	r.mu.Lock()
	r.closed = true
	all := make([]*session, 0, len(r.items))
	for _, s := range r.items {
		all = append(all, s)
	}
	r.mu.Unlock()
	for _, s := range all {
		s.close()
	}
}
