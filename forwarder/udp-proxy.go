package main

import (
	"fmt"
	"net"

	log "github.com/sirupsen/logrus"
)

type UDPProxy struct {
	config   ForwarderConfig
	local    *net.UDPConn
	replyTo  *net.UDPAddr
	remote   *net.UDPConn
	running  bool
	listener UDPProxyListener
}

type UDPProxyListener interface {
	HandleUplink([]byte, *net.UDPAddr)
	HandleDownlink([]byte, *net.UDPAddr)
}

func CreateUDPProxy(config ForwarderConfig) (*UDPProxy, error) {
	inst := &UDPProxy{
		config:  config,
		running: true,
	}

	// Start the local thread (the remote will be created on demand)
	err := inst.bindLocal()
	if err != nil {
		return nil, err
	}

	return inst, err
}

func (p *UDPProxy) SetListener(listener UDPProxyListener) {
	p.listener = listener
}

func (p *UDPProxy) bindLocal() error {
	if p.local != nil {
		p.local.Close()
		p.local = nil
	}

	s, err := net.ResolveUDPAddr("udp4", p.config.LocalAddress)
	if err != nil {
		return fmt.Errorf("Invalid address '%s' given: %w",
			p.config.LocalAddress, err)
	}

	c, err := net.ListenUDP("udp4", s)
	if err != nil {
		return fmt.Errorf("Could not bind to %s: %w",
			p.config.LocalAddress, err)
	}

	log.Infof("Listening for connctions on %s", p.config.LocalAddress)
	p.local = c

	// Start local thread
	go p.localThread()
	return nil
}

func (p *UDPProxy) localThread() {
	log.Debugf("Starting loal thread")
	for {
		b := make([]byte, p.config.BufferSize)
		log.Debugf("Reading up to %d bytes from %s", p.config.BufferSize, p.local.LocalAddr().String())
		n, addr, err := p.local.ReadFromUDP(b)
		log.Debugf("Received %d bytes from %s", n, addr.String())

		if !p.running {
			log.Debugf("Stopping local connection thread")
			return
		}
		if err != nil {
			log.Errorf("Unable to read from local socket: %s", err.Error())
			break
		}

		// Kep track the reply-to
		p.replyTo = addr

		if n > 0 {
			// If the remote side is disconnected, try to connect
			if p.remote == nil {
				err := p.connectRemote()
				if err != nil {
					log.Warnf("Could not connect to remote: %s", err.Error())
				}
			}

			// Forward data to remote
			log.Debugf("Writing %d bytes to %s", n, p.remote.RemoteAddr().String())
			_, err := p.remote.Write(b[0:n])
			if err != nil {
				log.Warnf("Could not write to remote: %s", err.Error())
			}

			// Also relay to listener
			if p.listener != nil {
				p.listener.HandleUplink(b[0:n], addr)
			}
		}
	}

	// If we exited the loop, it means something went wrong,
	// so we must try to re-bind. However if this fails, we
	// can do nothing else but panic.
	err := p.bindLocal()
	if err != nil {
		log.Fatalf("Unable to re-bind to local endpoint: %s", err.Error())
	}
}

func (p *UDPProxy) connectRemote() error {
	if p.remote != nil {
		p.remote.Close()
		p.remote = nil
	}

	// Bind to local address
	s, err := net.ResolveUDPAddr("udp4", p.config.RemoteAddress)
	if err != nil {
		return fmt.Errorf("Invalid address '%s' given: %w",
			p.config.RemoteAddress, err)
	}

	c, err := net.DialUDP("udp", nil, s)
	if err != nil {
		return fmt.Errorf("Could not connect to %s: %w",
			p.config.RemoteAddress, err)
	}

	log.Infof("Connected to remote endpoint %s", p.config.RemoteAddress)
	p.remote = c

	// Start remote thread
	go p.remoteThread()
	return nil
}

func (p *UDPProxy) remoteThread() {
	log.Debugf("Starting remote thread")
	for {
		b := make([]byte, p.config.BufferSize)
		log.Debugf("Reading up to %d bytes from %s", p.config.BufferSize, p.remote.RemoteAddr().String())
		n, addr, err := p.remote.ReadFromUDP(b)
		log.Debugf("Received %d bytes from %s", n, addr.String())

		if !p.running {
			log.Debugf("Stopping remote connection thread")
			return
		}
		if err != nil {
			log.Errorf("Unable to read from remote socket: %s", err.Error())
			break
		}

		if n > 0 {
			if p.local != nil {
				// Forward data to local
				log.Debugf("Writing %d bytes to %s", n, p.replyTo.String())
				_, err := p.local.WriteTo(b[0:n], p.replyTo)
				if err != nil {
					log.Warnf("Could not write to local socket: %s", err.Error())
				}
			} else {
				log.Warnf("Local socket not connected. Dropped %d bytes", n)
			}

			// Also relay to listener
			if p.listener != nil {
				p.listener.HandleDownlink(b[0:n], addr)
			}
		}
	}

	// If we exited the loop, it means something went wrong,
	// so we must try to re-bind. However if this fails, we
	// can do nothing else but panic.
	err := p.connectRemote()
	if err != nil {
		log.Fatalf("Unable to re-connect to remote endpoint: %s", err.Error())
	}
}
