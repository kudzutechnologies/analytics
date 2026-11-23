package main

import (
	"encoding/hex"
	"fmt"
	"net"
	"sync"

	log "github.com/sirupsen/logrus"
)

type ProxyStreamEvents struct {
	RemoteError  func(err error)
	LocalError   func(err error)
	DataReceived func(data []byte, rxFrom *net.UDPAddr)
}

type ProxyStreamConfig struct {
	Name              string
	Index             int
	BufferSize        int
	Local             *net.UDPConn
	LocalReplyAddress *net.UDPAddr
	RemoteAddress     *net.UDPAddr
	RemoteBindAddress *net.UDPAddr
	Events            ProxyStreamEvents
	workers           *sync.WaitGroup
}

type ProxyStream struct {
	mu              sync.Mutex
	connected       bool
	closed          bool
	closeWg         sync.WaitGroup
	remote          *net.UDPConn
	remoteBoundAddr *net.UDPAddr
	remoteReplyAddr *net.UDPAddr
	conf            *ProxyStreamConfig
}

var ErrSocketClosed = fmt.Errorf("trying to use a closed socket")

func strAddr(addr *net.UDPAddr) string {
	if addr == nil {
		return "0.0.0.0:0"
	}
	return addr.String()
}

func CreateProxyStream(config *ProxyStreamConfig) *ProxyStream {
	inst := &ProxyStream{
		closed: false,
		conf:   config,
	}

	return inst
}

func (s *ProxyStream) HandleLocalData(data []byte) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSocketClosed
	}

	// Make sure we are connected
	if !s.connected {
		log.Debugf("[%s] Remote not connected, connecting now", s.conf.Name)
		err := s.connectLocked()
		if err != nil {
			s.mu.Unlock()
			return err
		}
	}

	log.Debugf("[%s] Sending %d bytes to %s: %s", s.conf.Name,
		len(data), s.conf.RemoteAddress.String(), hex.EncodeToString(data))

	// The socket remains safe to use if Close runs after the snapshot.
	remote := s.remote
	s.mu.Unlock()
	_, err := remote.Write(data)
	if err != nil {
		log.Warnf("[%s] Unable to write to remote (%s): %s", s.conf.Name, s.conf.RemoteAddress.String(), err.Error())
		s.Close()
		s.conf.Events.RemoteError(err)
		return err
	}

	return nil
}

// stop closes the socket without joining the reader. It is safe in reader callbacks.
func (s *ProxyStream) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		if s.remote != nil {
			s.remote.Close()
		}
	}
}

func (s *ProxyStream) Close() {
	s.stop()
	s.closeWg.Wait()
}

func (s *ProxyStream) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *ProxyStream) remoteToLocal() {
	defer s.closeWg.Done()
	if s.conf.workers != nil {
		defer s.conf.workers.Done()
	}

	log.Debugf("[%s] Reading thread started", s.conf.Name)
	b := make([]byte, s.conf.BufferSize)

	for {
		log.Debugf("[%s] Reading up to %d bytes from %s", s.conf.Name, s.conf.BufferSize, s.remoteBoundAddr.String())
		n, addr, err := s.remote.ReadFromUDP(b)

		// If were intentionally closed in the process, exit the loop
		if s.isClosed() {
			break
		}

		// Otherwise handle errors
		if err != nil {
			log.Errorf("[%s] Could not read from remote side: %s", s.conf.Name, err.Error())
			s.stop()
			s.conf.Events.RemoteError(err)
			break
		}

		log.Debugf("[%s] Received %d bytes from %s: %s", s.conf.Name,
			n, s.remoteBoundAddr.String(), hex.EncodeToString(b[0:n]))

		// Once a message is received from the specified endpoint, we will be using that
		// for communicating with the upstream from this point onwards
		s.mu.Lock()
		s.remoteReplyAddr = addr
		s.mu.Unlock()

		// Send data to the local endpoint
		wb, err := s.conf.Local.WriteToUDP(b[0:n], s.conf.LocalReplyAddress)
		if err != nil {
			log.Warnf("[%s] Unable to write to local (%s): %s", s.conf.Name, s.conf.LocalReplyAddress.String(), err)
			s.stop()
			s.conf.Events.LocalError(err)
			break
		} else if wb != n {
			// We don't expect fragmentation, so just log this as a warning
			log.Warnf("[%s] Remote-to-local fragmentation (%d != %d)", s.conf.Name, wb, n)
		}

		// We can now handle data
		s.conf.Events.DataReceived(b[0:n], addr)
	}

	log.Debugf("[%s] Reading thread exited", s.conf.Name)
}

// connectLocked publishes the socket and registers its reader before Close can wait.
func (s *ProxyStream) connectLocked() error {
	var err error
	if s.connected {
		return nil
	}

	log.Debugf("[%s] Dialing %s (from %s)", s.conf.Name, s.conf.RemoteAddress.String(),
		strAddr(s.conf.RemoteBindAddress))

	// Connect on the remote socket
	s.remote, err = net.DialUDP("udp4", s.conf.RemoteBindAddress, s.conf.RemoteAddress)
	if err != nil {
		return fmt.Errorf(
			"could not connect to %s: %s",
			s.conf.RemoteAddress.String(),
			err,
		)
	}

	log.Infof("[%s] Connected to %s", s.conf.Name, s.conf.RemoteAddress.String())

	rlAddr := s.remote.LocalAddr()
	if addr, ok := rlAddr.(*net.UDPAddr); ok {
		s.remoteBoundAddr = addr
	} else {
		return fmt.Errorf("bound to unexpected local address")
	}

	// Start the reader thread
	s.connected = true
	s.closeWg.Add(1)
	if s.conf.workers != nil {
		s.conf.workers.Add(1)
	}
	go s.remoteToLocal()

	return nil
}
