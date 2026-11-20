package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	log "github.com/sirupsen/logrus"
)

type UDPProxy struct {
	mu           sync.Mutex
	eventsMu     sync.RWMutex
	idMu         sync.Mutex
	restartWg    sync.WaitGroup
	done         chan struct{}
	restarting   bool
	closed       bool
	closeWg      sync.WaitGroup
	upSock       *net.UDPConn
	dnSock       *net.UDPConn
	config       *UDPProxyConfig
	upStreams    *lru.Cache[string, *ProxyStream]
	dnStreams    *lru.Cache[string, *ProxyStream]
	streamIds    *lru.Cache[string, int]
	lastStreamId int
}

type UDPProxyConfig struct {
	UpListenAddr        *net.UDPAddr
	UpConnectAddr       *net.UDPAddr
	UpConnectBindAddr   *net.UDPAddr
	DownListenAddr      *net.UDPAddr
	DownConnectAddr     *net.UDPAddr
	DownConnectBindAddr *net.UDPAddr
	BufferSize          int
	SocketStreams       int
	ReconnectInterval   int
	Events              UDPProxyEvents
	DumpFile            *os.File
}

type UDPProxyEvents interface {
	UpLocalData([]byte, *net.UDPAddr)
	UpRemoteData([]byte, *net.UDPAddr)
	DnLocalData([]byte, *net.UDPAddr)
	DnRemoteData([]byte, *net.UDPAddr)
}

func CreateUDPProxy(config *UDPProxyConfig) (*UDPProxy, error) {
	var err error
	inst := &UDPProxy{
		config: config,
		done:   make(chan struct{}),
	}

	inst.upStreams, err = lru.NewWithEvict(config.SocketStreams, inst.evictStream)
	if err != nil {
		return nil, fmt.Errorf("could not allocate up-streams: %w", err)
	}
	inst.dnStreams, err = lru.NewWithEvict(config.SocketStreams, inst.evictStream)
	if err != nil {
		return nil, fmt.Errorf("could not allocate down-streams: %w", err)
	}
	inst.streamIds, err = lru.New[string, int](config.SocketStreams)
	if err != nil {
		return nil, fmt.Errorf("could not allocate indices: %w", err)
	}

	err = inst.bindLocal()
	if err != nil {
		inst.Close()
	}
	return inst, err
}

func (s *UDPProxy) Close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.done)
	}
	s.mu.Unlock()
	s.closeAll()
	s.restartWg.Wait()
	s.joinThreads()
}

func (s *UDPProxy) SetEventHandler(events UDPProxyEvents) {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	s.config.Events = events
}

func (s *UDPProxy) eventHandler() UDPProxyEvents {
	s.eventsMu.RLock()
	defer s.eventsMu.RUnlock()
	return s.config.Events
}

func (p *UDPProxy) writeDump(stream int, data []byte) {
	if p.config.DumpFile == nil {
		return
	}

	text := fmt.Sprintf("%d:%s\n", stream, base64.StdEncoding.EncodeToString(data))
	if _, err := p.config.DumpFile.WriteString(text); err != nil {
		log.Warnf("Error writing to dump file: %s", err.Error())
	} else {
		p.config.DumpFile.Sync()
	}
}

func (s *UDPProxy) evictStream(key string, stream *ProxyStream) {
	log.Debugf("[%s] Stream evicted", stream.conf.Name)
	stream.stop()
}

func (s *UDPProxy) bindLocal() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSocketClosed
	}
	var err error

	// Open first connection
	s.upSock, err = net.ListenUDP("udp", s.config.UpListenAddr)
	if err != nil {
		return fmt.Errorf("could not bind to %s for UP: %w", s.config.UpListenAddr.String(), err)
	}

	// Open second connection
	if s.config.DownListenAddr != nil {
		s.dnSock, err = net.ListenUDP("udp", s.config.DownListenAddr)
		if err != nil {
			s.upSock.Close()
			return fmt.Errorf("could not bind to %s for DOWN: %w", s.config.DownListenAddr.String(), err)
		}

		log.Infof("[dn] Listening on %s for downlinks", s.config.DownListenAddr.String())
	} else {
		log.Infof("[dn] Also listening on %s for downlinks", s.config.UpListenAddr.String())
	}

	s.restarting = false
	s.closeWg.Add(1)
	go s.upThread(s.upSock)
	if s.dnSock != nil {
		s.closeWg.Add(1)
		go s.dnThread(s.dnSock)
	}
	return nil
}

func (s *UDPProxy) closeAll() {
	s.mu.Lock()
	if s.upSock != nil {
		s.upSock.Close()
	}
	if s.dnSock != nil {
		s.dnSock.Close()
	}
	// Creation also holds mu, so no stream can be admitted after this purge.
	s.upStreams.Purge()
	s.dnStreams.Purge()
	s.mu.Unlock()
}

func (s *UDPProxy) stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed || s.restarting
}

func (s *UDPProxy) joinThreads() {
	log.Debugf("Waiting for running threads to join")
	s.closeWg.Wait()
}

func (s *UDPProxy) scheduleRestart() {
	s.mu.Lock()
	if s.closed || s.restarting {
		s.mu.Unlock()
		return
	}
	s.restarting = true
	s.restartWg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.restartWg.Done()
		s.closeAll()
		s.joinThreads()
		for {
			timer := time.NewTimer(time.Second * time.Duration(s.config.ReconnectInterval))
			select {
			case <-s.done:
				timer.Stop()
				return
			case <-timer.C:
			}
			if err := s.bindLocal(); err == nil {
				return
			} else {
				log.Warnf("Could not restart local sockets: %s", err)
			}
		}
	}()
}

func (s *UDPProxy) upThread(socket *net.UDPConn) {
	defer s.closeWg.Done()

	b := make([]byte, s.config.BufferSize)
	log.Debugf("[up] Started thread")

	for !s.stopped() {
		n, addr, err := socket.ReadFromUDP(b)
		if s.stopped() {
			break
		}

		if err != nil {
			log.Errorf("[up] Unable to read from local socket: %s", err.Error())
			s.scheduleRestart()
			break
		}

		stream := s.getUpStreamFor(addr, b[0:n])
		if stream == nil {
			break
		}
		s.writeDump(stream.conf.Index*2+0, b[0:n])

		err = stream.HandleLocalData(b[0:n])
		if err != nil {
			log.Warnf("[up:%s] Could not write to remote: %s", addr.String(), err.Error())
			log.Debugf("[up:%s] Evicting due to error", addr.String())
			s.upStreams.Remove(addr.String())
			continue
		} else {
			if events := s.eventHandler(); events != nil {
				events.UpLocalData(b[0:n], addr)
			}
		}
	}

	log.Debugf("[up] Exited thread")
}

func (s *UDPProxy) dnThread(socket *net.UDPConn) {
	defer s.closeWg.Done()

	b := make([]byte, s.config.BufferSize)
	log.Debugf("[dn] Started thread")

	for !s.stopped() {
		n, addr, err := socket.ReadFromUDP(b)
		if s.stopped() {
			break
		}

		if err != nil {
			log.Errorf("[dn] Unable to read from local socket: %s", err.Error())
			s.scheduleRestart()
			break
		}

		stream := s.getDnStreamFor(addr, b[0:n])
		if stream == nil {
			break
		}
		s.writeDump(stream.conf.Index*2+0, b[0:n])

		err = stream.HandleLocalData(b[0:n])
		if err != nil {
			log.Warnf("[up:%s] Could not write to remote: %s", addr.String(), err.Error())
			log.Debugf("[dn:%s] Evicting due to error", addr.String())
			s.dnStreams.Remove(addr.String())
			continue
		} else {
			if events := s.eventHandler(); events != nil {
				events.DnLocalData(b[0:n], addr)
			}
		}
	}

	log.Debugf("[dn] Exited thread")
}

func (s *UDPProxy) getStreamId(ip net.IP, idByes []byte) int {
	s.idMu.Lock()
	defer s.idMu.Unlock()
	key := ip.String()
	slot, ok := s.streamIds.Get(key)
	if !ok {
		slot = s.lastStreamId
		s.lastStreamId++
		s.streamIds.Add(key, slot)
	}

	if SemtechUDPIsUplink(idByes) {
		return slot + 0
	} else {
		return slot + 1
	}
}

func (s *UDPProxy) getUpStreamFor(addr *net.UDPAddr, idbytes []byte) *ProxyStream {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.restarting {
		return nil
	}
	key := addr.String()
	if found, ok := s.upStreams.Get(key); ok {
		return found
	}

	idx := s.getStreamId(addr.IP, idbytes)
	stream := CreateProxyStream(&ProxyStreamConfig{
		Name:              fmt.Sprintf("up:%s", key),
		Index:             idx,
		BufferSize:        s.config.BufferSize,
		Local:             s.upSock,
		LocalReplyAddress: addr,
		RemoteAddress:     s.config.UpConnectAddr,
		RemoteBindAddress: s.config.UpConnectBindAddr,
		workers:           &s.closeWg,
		Events: ProxyStreamEvents{
			DataReceived: func(data []byte, rxFrom *net.UDPAddr) {
				s.writeDump(idx*2+1, data)
				if events := s.eventHandler(); events != nil {
					events.UpRemoteData(data, addr)
				}
			},
			LocalError: func(err error) {
				s.scheduleRestart()
			},
			RemoteError: func(err error) {
				s.upStreams.Remove(key)
			},
		},
	})
	s.upStreams.Add(key, stream)

	return stream
}

func (s *UDPProxy) getDnStreamFor(addr *net.UDPAddr, idBytes []byte) *ProxyStream {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.restarting {
		return nil
	}
	key := addr.String()
	if found, ok := s.dnStreams.Get(key); ok {
		return found
	}

	idx := s.getStreamId(addr.IP, idBytes)
	stream := CreateProxyStream(&ProxyStreamConfig{
		Name:              fmt.Sprintf("dn:%s", key),
		BufferSize:        s.config.BufferSize,
		Local:             s.dnSock,
		LocalReplyAddress: addr,
		RemoteAddress:     s.config.DownConnectAddr,
		RemoteBindAddress: s.config.DownConnectBindAddr,
		workers:           &s.closeWg,
		Events: ProxyStreamEvents{
			DataReceived: func(data []byte, rxFrom *net.UDPAddr) {
				s.writeDump(idx*2+1, data)
				if events := s.eventHandler(); events != nil {
					events.DnRemoteData(data, addr)
				}
			},
			LocalError: func(err error) {
				s.scheduleRestart()
			},
			RemoteError: func(err error) {
				s.dnStreams.Remove(key)
			},
		},
	})
	s.dnStreams.Add(key, stream)

	return stream
}
