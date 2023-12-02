package main

import (
	"fmt"
	"net"
	"os"

	"github.com/kudzutechnologies/analytics/client"
	log "github.com/sirupsen/logrus"
)

func main() {
	// Parse configuration from environment
	config := ParseConfigFromEnv()

	// Connect to the analytics endpoint
	client := client.CreateAnalyticsClient(client.AnalyticsClientConfig{
		ClientId:            config.ClientId,
		ClientKey:           config.ClientKey,
		Endpoint:            config.Endpoint,
		ConnectTimeout:      int32(config.ConnectTimeout),
		RequestTimeout:      int32(config.RequestTimeout),
		MaxReconnectBackoff: int32(config.MaxReconnectBackoff),
		ServerSide:          &config.ServerSide,
	})

	// Create the UDP proxy
	proxy, err := CreateUDPProxy(CreateUDPProxyConfig(config))
	if err != nil {
		log.Fatalf("Could not start forwarder: %s", err.Error())
	}

	// Try to connect to the analytics endpoint
	fw := CreateAnalyticsForwarder(config, client, proxy)
	fw.StartAndWait()
}

func parseEndpoint(name string, host string, port int) *net.UDPAddr {
	log.Debugf("Using %s endpoint: %s:%d", name, host, port)
	ep, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		log.Fatalf("Invalid %s endpoint: %s:%d: %s", name, host, port, err.Error())
	}
	return ep
}

func CreateUDPProxyConfig(config ForwarderConfig) *UDPProxyConfig {
	var err error
	var dnListen *net.UDPAddr = nil
	var dnConnect *net.UDPAddr = nil
	var dmpFile *os.File = nil

	upListen := parseEndpoint("local uplink", config.ListenHost, config.ListenPortUp)
	if config.ListenPortDown != config.ListenPortUp {
		dnListen = parseEndpoint("local downlink", config.ListenHost, config.ListenPortDown)
	} else {
		log.Debugf("Using same endpoint for downlink: %s:%d", config.ListenHost, config.ListenPortUp)
	}

	upConnect := parseEndpoint("remote uplink", config.ConnectHost, config.ConnectPortUp)
	if config.ListenPortDown != config.ListenPortUp {
		dnListen = parseEndpoint("remote downlink", config.ConnectHost, config.ConnectPortDown)
	} else {
		log.Debugf("Using same endpoint for downlink: %s:%d", config.ConnectHost, config.ConnectPortUp)
	}

	bindAddr := parseEndpoint("remote bind", config.ConnectInterface, 0)

	if config.DebugDump != "" {
		dmpFile, err = os.OpenFile(config.DebugDump, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			log.Warnf("Could not open %s: %s", config.DebugDump, err.Error())
		} else {
			log.Infof("Writing all traffic to %s", config.DebugDump)
		}
	}

	ret := &UDPProxyConfig{
		UpListenAddr:        upListen,
		UpConnectAddr:       upConnect,
		UpConnectBindAddr:   bindAddr,
		DownListenAddr:      dnListen,
		DownConnectAddr:     dnConnect,
		DownConnectBindAddr: bindAddr,
		BufferSize:          config.BufferSize,
		SocketStreams:       config.MaxUDPStreams,
		ReconnectInterval:   config.RequestTimeout,
		DumpFile:            dmpFile,
	}

	return ret
}
