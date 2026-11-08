package main

import (
	"net/http"
	"os"

	"github.com/kudzutechnologies/analytics/client"
	log "github.com/sirupsen/logrus"
)

func main() {
	config := ParseConfigFromEnv()

	analyticsClient := client.CreateAnalyticsClient(client.AnalyticsClientConfig{
		ClientId:            config.ClientId,
		ClientKey:           config.ClientKey,
		Endpoint:            config.Endpoint,
		CAFile:              config.CAFile,
		ConnectTimeout:      int32(config.ConnectTimeout),
		RequestTimeout:      int32(config.RequestTimeout),
		MaxReconnectBackoff: int32(config.MaxReconnectBackoff),
		ServerSide:          &config.ServerSide,
	})

	var certStore *CertStore
	if config.CertsDir != "" {
		var err error
		certStore, err = NewCertStore(config.CertsDir, config.CertsCACert, config.CertsCAKey)
		if err != nil {
			log.Fatalf("Cert store: %v", err)
		}
		// When no explicit listen TLS cert is provided, use bootstrap server cert from certs-dir.
		if config.ListenTLSCert == "" && config.ListenTLSKey == "" {
			if _, err := certStore.GetOrCreateBootstrap(); err != nil {
				log.Fatalf("Bootstrap cert: %v", err)
			}
			config.ListenTLSCert = certStore.BootstrapCertPath()
			config.ListenTLSKey = certStore.BootstrapKeyPath()
			log.Infof("Using bootstrap server cert from %s", config.CertsDir)
		}
	}

	if config.ListenTLSCert != "" && log.IsLevelEnabled(log.DebugLevel) {
		if b, err := os.ReadFile(config.ListenTLSCert); err == nil {
			loadAndLogCertsFromPEM("listen-tls", b)
		}
	}

	var dump *DebugDumpWriter
	if config.DebugDump != "" {
		f, err := os.OpenFile(config.DebugDump, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			log.Warnf("Could not open debug dump %s: %v", config.DebugDump, err)
		} else {
			dump = NewDebugDumpWriter(f)
			log.Infof("Writing all traffic to %s", config.DebugDump)
		}
	}

	proxyConfig := &WSProxyConfig{
		ListenHost:               config.ListenHost,
		ListenPort:               config.ListenPort,
		ConnectURI:               config.ConnectURI,
		ConnectDiscoveryURI:      config.ConnectDiscoveryURI,
		ConnectDiscoveryPath:     config.ConnectDiscoveryPath,
		ConnectDataPath:          config.ConnectDataPath,
		ConnectDataURI:           config.ConnectDataURI,
		ConnectWSSubprotocol:     config.ConnectWSSubprotocol,
		ListenTLSCert:            config.ListenTLSCert,
		ListenTLSKey:             config.ListenTLSKey,
		ConnectTLSCA:             config.ConnectTLSCA,
		ConnectTLSInsecure:       config.ConnectTLSInsecure,
		ConnectDataTLSServerName: config.ConnectDataTLSServerName,
		ConnectDataTLSCA:         config.ConnectDataTLSCA,
		ConnectDataTLSInsecure:   config.ConnectDataTLSInsecure,
		MaxStreams:               config.MaxWSStreams,
		Events:                   nil,
		DebugDump:                dump,
		CertStore:                certStore,
		CertsUpstreamClientCert:  config.CertsUpstreamClientCert,
		Verbose:                  config.Verbose,
	}
	proxy, err := NewWSProxy(proxyConfig)
	if err != nil {
		log.Fatalf("Could not start WebSocket proxy: %v", err)
	}

	forwarder := CreateAnalyticsForwarder(config, analyticsClient, proxy)
	proxy.SetEvents(forwarder)

	go func() {
		if err := proxy.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("WebSocket proxy: %v", err)
		}
	}()

	forwarder.StartAndWait()
}
