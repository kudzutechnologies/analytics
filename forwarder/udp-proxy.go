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
	}

	inst.upStreams, err = lru.NewWithEvict(config.SocketStreams, inst.evictStream)
	if err != nil {
		return nil, fmt.Errorf("Could not allocate up-streams: %w", err)
	}
	inst.dnStreams, err = lru.NewWithEvict(config.SocketStreams, inst.evictStream)
	if err != nil {
		return nil, fmt.Errorf("Could not allocate down-streams: %w", err)
	}
	inst.streamIds, err = lru.New[string, int](config.SocketStreams)
	if err != nil {
		return nil, fmt.Errorf("Could not allocate indices: %w", err)
	}

	err = inst.bindLocal()
	return inst, err
}

func (s *UDPProxy) Close() {
	s.closeAll()
	s.joinThreads()
}

func (s *UDPProxy) SetEventHandler(events UDPProxyEvents) {
	s.config.Events = events
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
	stream.Close()
}

func (s *UDPProxy) bindLocal() error {
	var err error
	s.closed = false

	// Open first connection
	s.upSock, err = net.ListenUDP("udp", s.config.UpListenAddr)
	if err != nil {
		return fmt.Errorf("Could not bind to %s for UP: %w", s.config.UpListenAddr.String(), err)
	}

	s.closeWg.Add(1)
	go s.upThread()
	log.Infof("[up] Listening on %s for uplinks", s.config.UpListenAddr.String())

	// Open second connection
	if s.config.DownListenAddr != nil {
		s.dnSock, err = net.ListenUDP("udp", s.config.DownListenAddr)
		if err != nil {
			return fmt.Errorf("Could not bind to %s for DOWN: %w", s.config.DownListenAddr.String(), err)
		}

		s.closeWg.Add(1)
		go s.dnThread()
		log.Infof("[dn] Listening on %s for downlinks", s.config.DownListenAddr.String())
	} else {
		log.Infof("[dn] Also listening on %s for downlinks", s.config.UpListenAddr.String())
	}

	return nil
}

func (s *UDPProxy) bindLocalWithBackoff() {
	err := s.bindLocal()
	if err != nil {
		log.Warnf("Could not start local sockets: %s", err.Error())
		s.scheduleRestart()
	}
}

func (s *UDPProxy) closeAll() {
	if s.closed {
		return
	}

	s.closed = true
	if s.upSock != nil {
		log.Debugf("[up] Closing socket")
		s.upSock.Close()
	}
	if s.dnSock != nil {
		log.Debugf("[dn] Closing socket")
		s.dnSock.Close()
	}

	log.Debugf("[up] Purging %d streams", s.upStreams.Len())
	s.upStreams.Purge()
	log.Debugf("[dn] Purging %d streams", s.dnStreams.Len())
	s.dnStreams.Purge()
}

func (s *UDPProxy) joinThreads() {
	log.Debugf("Waiting for running threads to join")
	s.closeWg.Wait()
}

func (s *UDPProxy) scheduleRestart() {
	if s.closed {
		return
	}

	log.Infof("Restarting sockets in %d seconds", s.config.ReconnectInterval)
	s.closeAll()

	go func() {
		// We might be called from within the thread, so we should not try to
		// join in the same stack frame. However we should wait before we try
		// to re-start again...
		s.joinThreads()

		time.Sleep(time.Second * time.Duration(s.config.ReconnectInterval))
		s.bindLocalWithBackoff()
	}()
}

func (s *UDPProxy) upThread() {
	defer s.closeWg.Done()

	b := make([]byte, s.config.BufferSize)
	log.Debugf("[up] Started thread")

	for !s.closed {
		n, addr, err := s.upSock.ReadFromUDP(b)
		if s.closed {
			break
		}

		if err != nil {
			log.Errorf("[up] Unable to read from local socket: %s", err.Error())
			s.scheduleRestart()
			break
		}

		stream := s.getUpStreamFor(addr, b[0:n])
		s.writeDump(stream.conf.Index*2+0, b[0:n])

		err = stream.HandleLocalData(b[0:n])
		if err != nil {
			log.Warnf("[up:%s] Could not write to remote: %s", addr.String(), err.Error())
			log.Debugf("[up:%s] Evicting due to error", addr.String())
			s.upStreams.Remove(addr.String())
			break
		} else {
			if s.config.Events != nil {
				s.config.Events.UpLocalData(b[0:n], addr)
			}
		}
	}

	log.Debugf("[up] Exited thread")
}

func (s *UDPProxy) dnThread() {
	defer s.closeWg.Done()

	b := make([]byte, s.config.BufferSize)
	log.Debugf("[dn] Started thread")

	for !s.closed {
		n, addr, err := s.dnSock.ReadFromUDP(b)
		if s.closed {
			break
		}

		if err != nil {
			log.Errorf("[dn] Unable to read from local socket: %s", err.Error())
			s.scheduleRestart()
			break
		}

		stream := s.getDnStreamFor(addr, b[0:n])
		s.writeDump(stream.conf.Index*2+0, b[0:n])

		err = stream.HandleLocalData(b[0:n])
		if err != nil {
			log.Warnf("[up:%s] Could not write to remote: %s", addr.String(), err.Error())
			log.Debugf("[dn:%s] Evicting due to error", addr.String())
			s.dnStreams.Remove(addr.String())
			break
		} else {
			if s.config.Events != nil {
				s.config.Events.DnLocalData(b[0:n], addr)
			}
		}
	}

	log.Debugf("[dn] Exited thread")
}

func (s *UDPProxy) getStreamId(ip net.IP, idByes []byte) int {
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
		Events: ProxyStreamEvents{
			DataReceived: func(data []byte, rxFrom *net.UDPAddr) {
				s.writeDump(idx*2+1, data)
				if s.config.Events != nil {
					s.config.Events.UpRemoteData(data, addr)
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
		Events: ProxyStreamEvents{
			DataReceived: func(data []byte, rxFrom *net.UDPAddr) {
				s.writeDump(idx*2+1, data)
				if s.config.Events != nil {
					s.config.Events.DnRemoteData(data, addr)
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
