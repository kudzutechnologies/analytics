package main

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type sessionDiagnostics struct {
	mu                                                       sync.Mutex
	gatewayMessages, upstreamMessages                        uint64
	gatewayPings, gatewayPongs, upstreamPings, upstreamPongs uint64
	versionForwarded, routerConfigForwarded                  bool
}

func (s *session) isClosed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// These handlers run in this connection's reader goroutine. Keep the default
// ping reply (with the same payload), and count all incoming activity as live.
func (s *session) installKeepalive(conn *websocket.Conn, side string) {
	conn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
	pingHandler := conn.PingHandler()
	conn.SetPingHandler(func(data string) error {
		s.stats.mu.Lock()
		if side == "gateway" {
			s.stats.gatewayPings++
		} else {
			s.stats.upstreamPings++
		}
		s.stats.mu.Unlock()
		if err := conn.SetReadDeadline(time.Now().Add(s.config.PongTimeout)); err != nil {
			return err
		}
		return pingHandler(data)
	})
	conn.SetPongHandler(func(string) error {
		s.stats.mu.Lock()
		if side == "gateway" {
			s.stats.gatewayPongs++
		} else {
			s.stats.upstreamPongs++
		}
		s.stats.mu.Unlock()
		return conn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
	})
}

func (s *session) recordForwarded(side string, kind int, raw []byte) {
	msgtype := ""
	if kind == websocket.TextMessage {
		msgtype, _ = GetMsgtype(raw)
	}
	s.stats.mu.Lock()
	version, routerConfig := false, false
	if side == "gateway" {
		s.stats.gatewayMessages++
		if msgtype == "version" && !s.stats.versionForwarded {
			s.stats.versionForwarded = true
			version = true
		}
	} else {
		s.stats.upstreamMessages++
		if msgtype == "router_config" && !s.stats.routerConfigForwarded {
			s.stats.routerConfigForwarded = true
			routerConfig = true
		}
	}
	s.stats.mu.Unlock()
	if version {
		log.Infof("%sLNS version forwarded age_s=%.1f", gatewayLogPrefix(s.gatewayKey), s.ageSeconds())
	}
	if routerConfig {
		log.Infof("%sLNS router_config forwarded age_s=%.1f", gatewayLogPrefix(s.gatewayKey), s.ageSeconds())
	}
}

func (s *session) ageSeconds() float64 {
	if s.started.IsZero() {
		return 0
	}
	return time.Since(s.started).Seconds()
}

func websocketReadFailureReason(err error) string {
	var network net.Error
	var closed *websocket.CloseError
	switch {
	case errors.As(err, &network) && network.Timeout():
		return "read_timeout"
	case errors.Is(err, net.ErrClosed):
		return "socket_closed"
	case errors.As(err, &closed):
		return fmt.Sprintf("websocket_close_%d", closed.Code)
	default:
		return "read_error"
	}
}

func (s *session) logReadFailure(side string, err error) {
	if s.isClosed() {
		return
	}
	log.Warnf("%s%s WebSocket read ended reason=%s age_s=%.1f", gatewayLogPrefix(s.gatewayKey), side, websocketReadFailureReason(err), s.ageSeconds())
}

func (s *session) logStatus(state string) {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()
	log.Infof("%sLNS session state=%s age_s=%.1f gateway_messages=%d upstream_messages=%d version_forwarded=%t router_config_forwarded=%t gateway_pings=%d gateway_pongs=%d upstream_pings=%d upstream_pongs=%d",
		gatewayLogPrefix(s.gatewayKey), state, s.ageSeconds(), s.stats.gatewayMessages, s.stats.upstreamMessages, s.stats.versionForwarded, s.stats.routerConfigForwarded,
		s.stats.gatewayPings, s.stats.gatewayPongs, s.stats.upstreamPings, s.stats.upstreamPongs)
}

func (p *WSProxy) logSessionStatus() {
	sessions := p.sessions.Values()
	log.Infof("LNS sessions active=%d", len(sessions))
	for _, s := range sessions {
		s.logStatus("open")
	}
}
