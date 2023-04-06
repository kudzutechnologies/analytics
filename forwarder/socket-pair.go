package main

import (
	"fmt"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

var NotConnectedError = fmt.Errorf("Not connected")
var IncompleteDataError = fmt.Errorf("Not connected")

type SocketPairHandlers struct {
	HandleUpRx func([]byte, *net.UDPAddr)
	HandleDnRx func([]byte, *net.UDPAddr)
	IsDnPacket func([]byte) bool
}

type SocketPairConfig struct {
	Name              string
	UpEndpoint        string
	DnEndpoint        string
	BufferSize        int
	RetryConnect      bool
	ReconnectInterval int
	ConnectInterface  string
}

type SocketPair struct {
	dn           *net.UDPConn
	dnEp         *net.UDPAddr
	dnReplyTo    *net.UDPAddr
	up           *net.UDPConn
	upEp         *net.UDPAddr
	upReplyTo    *net.UDPAddr
	conIface     *net.UDPAddr
	config       SocketPairConfig
	open         bool
	reconnecting bool
	same         bool
	listening    bool
	handlers     *SocketPairHandlers
	mu           sync.Mutex
}

func CreateSocketPair(config SocketPairConfig) (*SocketPair, error) {
	dnAddr, err := net.ResolveUDPAddr("udp4", config.DnEndpoint)
	if err != nil {
		return nil, fmt.Errorf("Could not parse address %s: %w", config.DnEndpoint, err)
	}
	upAddr, err := net.ResolveUDPAddr("udp4", config.UpEndpoint)
	if err != nil {
		return nil, fmt.Errorf("Could not parse address %s: %w", config.UpEndpoint, err)
	}
	conIface, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:0", config.ConnectInterface))
	if err != nil {
		return nil, fmt.Errorf("Could not parse connect address %s: %w", config.DnEndpoint, err)
	}

	return &SocketPair{
		dnEp:     dnAddr,
		upEp:     upAddr,
		conIface: conIface,
		config:   config,
		same:     config.DnEndpoint == config.UpEndpoint,
	}, nil
}

func (p *SocketPair) IsOpen() bool {
	return p.open
}

func (p *SocketPair) Listen() error {
	// No-op if already connected
	if p.open {
		return nil
	}

	var err error
	p.dnReplyTo = nil
	p.dn, err = net.ListenUDP("udp4", p.dnEp)
	if err != nil {
		return fmt.Errorf("Could not listen on %s: %w", p.dnEp.String(), err)
	}

	if !p.same {
		p.upReplyTo = nil
		p.up, err = net.ListenUDP("udp4", p.upEp)
		if err != nil {
			return fmt.Errorf("Could not connect to %s: %w", p.upEp.String(), err)
		}
	}

	p.listening = true
	p.open = true

	go p.dnThread()
	if !p.same {
		go p.upThread()
	}
	return nil
}

func (p *SocketPair) Connect() error {
	// No-op if already connected
	if p.open {
		return nil
	}

	// First use connect to test link feasibility
	var err error
	p.dnReplyTo = p.dnEp
	p.dn, err = net.DialUDP("udp4", p.conIface, p.dnEp)
	if err != nil {
		return fmt.Errorf("Could not listen on %s: %w", p.dnEp.String(), err)
	}

	// Then use listen to receive packets
	p.dn.Close()
	p.dn, err = net.ListenUDP("udp4", p.conIface)
	if err != nil {
		return fmt.Errorf("Could not create receiving socket for %s: %w", p.dnEp.String(), err)
	}

	if !p.same {
		p.upReplyTo = p.upEp
		p.up, err = net.DialUDP("udp4", p.conIface, p.upEp)
		if err != nil {
			return fmt.Errorf("Could not connect to %s: %w", p.upEp.String(), err)
		}
		p.up.Close()
		p.up, err = net.ListenUDP("udp4", p.conIface)
		if err != nil {
			return fmt.Errorf("Could not create receiving socket for %s: %w", p.upEp.String(), err)
		}
	}

	p.listening = false
	p.open = true

	go p.dnThread()
	if !p.same {
		go p.upThread()
	}
	return nil
}

func (p *SocketPair) Close() error {
	// No-op if already closed
	if !p.open {
		return nil
	}
	p.open = false

	if p.up != nil {
		p.up.Close()
		p.upReplyTo = nil
		p.up = nil
	}

	if p.dn != nil {
		p.dn.Close()
		p.dnReplyTo = nil
		p.dn = nil
	}

	return nil
}

func (p *SocketPair) Reconnect() error {
	err := p.Close()
	if err != nil {
		return fmt.Errorf("Could not close: %w", err)
	}

	err = p.Connect()
	if err != nil {
		return fmt.Errorf("Could not connect: %w", err)
	}

	return nil
}

func (p *SocketPair) SetHandlers(handlers *SocketPairHandlers) {
	p.handlers = handlers
}

func (p *SocketPair) tryReconnect(err error) {
	if p.config.RetryConnect {
		log.Warnf("[%s] Closed due to error, will reconnect: %s", p.config.Name, err.Error())
		go p.retryConnect()
	} else {
		log.Fatalf("[%s] Closed due to error: %s", p.config.Name, err.Error())
	}
}

func (p *SocketPair) retryConnect() {
	// Even if reconnect is called multiple times, only one
	// should be running at the time.
	p.mu.Lock()
	if p.reconnecting {
		p.mu.Unlock()
		return
	}
	p.reconnecting = true
	p.mu.Unlock()

	// Sleep for a sec
	log.Debugf("[%s] Reconnecting in", p.config.Name)
	time.Sleep(time.Second * time.Duration(p.config.ReconnectInterval))

	// Try to reconnect
	err := p.Connect()
	if err != nil {
		p.tryReconnect(err)
	}
}

func (p *SocketPair) ud() string {
	t := "dn"
	if p.same {
		t = "u/d"
	}
	return t
}

func (p *SocketPair) dnThread() {
	b := make([]byte, p.config.BufferSize)

	for p.open {
		log.Debugf("[%s:%s] Reading up to %d bytes from %s", p.config.Name, p.ud(), p.config.BufferSize, p.dnEp.String())
		n, addr, err := p.dn.ReadFromUDP(b)
		log.Debugf("[%s:%s] Received %d bytes from %s", p.config.Name, p.ud(), n, addr.String())

		if !p.open {
			break
		} else if err != nil {
			log.Debugf("[%s:%s] Unable to read from dn socket: %s", p.config.Name, p.ud(), err.Error())
			p.tryReconnect(err)
			break
		}

		if n > 0 {
			// If we have handlers, call-out now
			if p.handlers != nil {
				data := b[0:n]

				// If we are using only one socket for both traffic, try to
				// disambiguate the origin
				if p.same {
					dn := p.handlers.IsDnPacket(data)
					if dn {
						p.handlers.HandleDnRx(data, addr)
						p.dnReplyTo = addr
					} else {
						p.handlers.HandleUpRx(data, addr)
						p.upReplyTo = addr
					}
				} else {
					p.handlers.HandleDnRx(data, addr)
					p.dnReplyTo = addr
				}
			}
		}
	}

	log.Debugf("[%s:dn] Thread exited", p.config.Name)
}

func (p *SocketPair) WriteDn(data []byte) error {
	var (
		n   int
		err error
	)
	if p.dn != nil {
		if p.listening {
			log.Debugf("[%s:dn] Writing %d bytes to %s", p.config.Name, len(data), p.dnReplyTo.String())
			n, err = p.dn.WriteToUDP(data, p.dnReplyTo)
		} else {
			log.Debugf("[%s:dn] Writing %d bytes to %s", p.config.Name, len(data), p.dnEp.String())
			n, err = p.dn.WriteToUDP(data, p.dnEp)
		}
		if err != nil {
			log.Warnf("[%s:dn] Error writing data: %s", p.config.Name, err.Error())
			return err
		}
		if n != len(data) {
			log.Warnf("[%s:dn] Could not write entire buffer: %d remains", p.config.Name, len(data)-n)
			return IncompleteDataError
		}
		return nil
	}

	return NotConnectedError
}

func (p *SocketPair) upThread() {
	b := make([]byte, p.config.BufferSize)
	for p.open {
		log.Debugf("[%s:up] Reading up to %d bytes from %s", p.config.Name, p.config.BufferSize, p.upEp.String())
		n, addr, err := p.up.ReadFromUDP(b)
		log.Debugf("[%s:up] Received %d bytes from %s", p.config.Name, n, addr.String())

		// Handle error cases
		if !p.open {
			break
		} else if err != nil {
			log.Debugf("[%s:up] Unable to read from up socket: %s", p.config.Name, err.Error())
			p.tryReconnect(err)
			break
		}

		if n > 0 {
			// Keep track of the address to reply to
			if p.upReplyTo == nil {
				p.upReplyTo = addr
			}

			// If we have handlers, call-out now
			if p.handlers != nil {
				data := b[0:n]
				p.handlers.HandleUpRx(data, addr)
			}
		}
	}

	log.Debugf("[%s:up] Thread exited", p.config.Name)
}

func (p *SocketPair) WriteUp(data []byte) error {
	var (
		n   int
		err error
	)

	// Check if sockets are same
	sock := p.up
	sockEp := p.upEp
	if p.same {
		sock = p.dn
		sockEp = p.dnEp
	}

	if sock != nil {
		if p.listening {
			log.Debugf("[%s:up] Writing %d bytes to %s", p.config.Name, len(data), p.upReplyTo.String())
			n, err = sock.WriteToUDP(data, p.upReplyTo)
		} else {
			log.Debugf("[%s:up] Writing %d bytes to %s", p.config.Name, len(data), sockEp.String())
			n, err = sock.WriteToUDP(data, sockEp)
		}
		if err != nil {
			log.Warnf("[%s:up] Error writing data: %s", p.config.Name, err.Error())
			return err
		}
		if n != len(data) {
			log.Warnf("[%s:up] Could not write entire buffer: %d remains", p.config.Name, len(data)-n)
			return IncompleteDataError
		}
		return nil
	}

	return NotConnectedError
}
