package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"

	log "github.com/sirupsen/logrus"
)

type UDPProxy struct {
	config   ForwarderConfig
	local    *SocketPair
	remote   *SocketPair
	running  bool
	listener UDPProxyListener
	dumpfile *os.File
}

type UDPProxyListener interface {
	HandleUplink([]byte, *net.UDPAddr)
	HandleDownlink([]byte, *net.UDPAddr)
}

func CreateUDPProxy(config ForwarderConfig) (*UDPProxy, error) {
	local, err := CreateSocketPair(SocketPairConfig{
		Name:              "local",
		UpEndpoint:        fmt.Sprintf("%s:%d", config.ListenHost, config.ListenPortUp),
		DnEndpoint:        fmt.Sprintf("%s:%d", config.ListenHost, config.ListenPortDown),
		BufferSize:        config.BufferSize,
		RetryConnect:      true,
		ReconnectInterval: 1,
	})
	if err != nil {
		return nil, err
	}

	remote, err := CreateSocketPair(SocketPairConfig{
		Name:              "remote",
		ConnectInterface:  config.ConnectInterface,
		UpEndpoint:        fmt.Sprintf("%s:%d", config.ConnectHost, config.ConnectPortUp),
		DnEndpoint:        fmt.Sprintf("%s:%d", config.ConnectHost, config.ConnectPortDown),
		BufferSize:        config.BufferSize,
		RetryConnect:      true,
		ReconnectInterval: 1,
	})
	if err != nil {
		return nil, err
	}

	inst := &UDPProxy{
		config:  config,
		local:   local,
		remote:  remote,
		running: true,
	}

	// Connect the local sockets (the remote will be created on demand)
	local.SetHandlers(&SocketPairHandlers{
		HandleUpRx: func(b []byte, u *net.UDPAddr) {
			inst.handleLocalUpRx(b, u)
		},
		HandleDnRx: func(b []byte, u *net.UDPAddr) {
			inst.handleLocalDnRx(b, u)
		},
		IsDnPacket: SemtechUDPIsDownlink,
	})
	remote.SetHandlers(&SocketPairHandlers{
		HandleUpRx: func(b []byte, u *net.UDPAddr) {
			inst.handleRemoteUpRx(b, u)
		},
		HandleDnRx: func(b []byte, u *net.UDPAddr) {
			inst.handleRemoteDnRx(b, u)
		},
		IsDnPacket: SemtechUDPIsDownlink,
	})

	log.Infof("Listening for forwarder traffic on %s/%s", local.upEp.String(), local.dnEp.String())
	err = local.Listen()
	if err != nil {
		return nil, err
	}

	// If we are dumping traffic, open the file now
	if config.DebugDump != "" {
		inst.dumpfile, err = os.OpenFile(config.DebugDump, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			log.Warnf("Could not open %s: %s", config.DebugDump, err.Error())
		} else {
			log.Infof("Writing all traffic to %s", config.DebugDump)
		}
	}

	return inst, err
}

func (p *UDPProxy) handleLocalUpRx(dat []byte, addr *net.UDPAddr) {
	p.writeDump(0, dat)

	// Forward data to the remote endpoint
	if p.connectRemote() == nil {
		err := p.remote.WriteUp(dat)
		if err != nil {
			log.Warnf("Error writing to server: %s", err.Error())
		}
	}

	// Handle uplink
	if p.listener != nil {
		p.listener.HandleUplink(dat, addr)
	}
}

func (p *UDPProxy) handleLocalDnRx(dat []byte, addr *net.UDPAddr) {
	p.writeDump(1, dat)

	// Forward data to the remote endpoint
	if p.connectRemote() == nil {
		err := p.remote.WriteDn(dat)
		if err != nil {
			log.Warnf("Error writing to server: %s", err.Error())
		}
	}
}

func (p *UDPProxy) handleRemoteUpRx(dat []byte, addr *net.UDPAddr) {
	p.writeDump(2, dat)

	// Forward data to local endpoint
	p.local.WriteUp(dat)
}

func (p *UDPProxy) handleRemoteDnRx(dat []byte, addr *net.UDPAddr) {
	p.writeDump(3, dat)

	// Forward data to local endpoint
	p.local.WriteDn(dat)

	// Handle downlink
	if p.listener != nil {
		p.listener.HandleDownlink(dat, addr)
	}
}

func (p *UDPProxy) writeDump(stream int, data []byte) {
	if p.dumpfile == nil {
		return
	}

	text := fmt.Sprintf("%d:%s\n", stream, base64.StdEncoding.EncodeToString(data))
	if _, err := p.dumpfile.WriteString(text); err != nil {
		log.Warnf("Error writing to dump file: %s", err.Error())
	} else {
		p.dumpfile.Sync()
	}
}

func (p *UDPProxy) SetListener(listener UDPProxyListener) {
	p.listener = listener
}

func (p *UDPProxy) connectRemote() error {
	if p.remote.IsOpen() {
		return nil
	}

	log.Infof("Connecting to LoRaWAN server on %s/%s", p.remote.dnEp.String(), p.remote.dnEp.String())
	return p.remote.Connect()
}
