package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kudzutechnologies/analytics/client"
	log "github.com/sirupsen/logrus"
)

func main() { os.Exit(runMain()) }
func runMain() int {
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
		if config.CUPSMode == "managed" || config.CertsUpstreamClientCert {
			certStore, err = NewCertStore(config.CertsDir, config.CertsCACert, config.CertsCAKey)
		} else {
			certStore, err = OpenCredentialStore(config.CertsDir)
		}
		if err != nil {
			log.Fatalf("Cert store: %v", err)
		}

	}

	if config.ListenTLSCert != "" && log.IsLevelEnabled(log.DebugLevel) {
		if b, err := os.ReadFile(config.ListenTLSCert); err == nil {
			loadAndLogCertsFromPEM("listen-tls", b)
		}
	}

	var dump *DebugDumpWriter
	if config.DebugDump != "" && config.DebugDumpMaxBytes > 0 {
		f, err := os.OpenFile(config.DebugDump, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			log.Warnf("Could not open debug dump %s: %v", config.DebugDump, err)
		} else {
			defer f.Close()
			st, err := f.Stat()
			if err != nil || !st.Mode().IsRegular() {
				log.Fatal("Debug dump requires a regular file")
			}
			if err := f.Chmod(0600); err != nil {
				log.Fatal("Could not restrict debug dump permissions")
			}
			dump = NewDebugDumpWriter(f, int64(config.DebugDumpMaxBytes)-st.Size())
			log.Infof("Writing bounded diagnostic traffic to %s", config.DebugDump)
		}
	}

	proxyConfig := &WSProxyConfig{
		CUPSMaxCredentialBytes: config.CUPSMaxCredentialBytes,
		ProxyMode:              config.ProxyMode, PublicURI: config.PublicURI, GatewayAuthFile: config.GatewayAuthFile,
		LocalRouter: config.GatewayEUI, UpstreamAllowedOrigins: config.UpstreamAllowedOrigins, CUPSMode: config.CUPSMode,
		InjectRouterIntoVersion:  config.InjectRouterIntoVersion,
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- proxy.Start() }()
	forwarder.Start()
	statusTicker := time.NewTicker(time.Minute)
	defer statusTicker.Stop()
	code := 0
	running := true
	for running {
		select {
		case <-ctx.Done():
			running = false
		case err := <-serverErrors:
			if err != nil && err != http.ErrServerClosed {
				log.Error(err)
				code = 1
			}
			running = false
		case <-statusTicker.C:
			logDeliveryStatus("running", forwarder.DeliveryStats())
			proxy.logSessionStatus()
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), time.Duration(config.ShutdownDeadlineMS)*time.Millisecond)
	defer cancel()
	_ = proxy.Shutdown(shutdown)
	if !forwarder.Stop(shutdown) && code == 0 {
		code = 2
	}
	logDeliveryStatus("stopped", forwarder.DeliveryStats())
	return code
}

func logDeliveryStatus(state string, s DeliveryStats) {
	log.Infof("%s retained_messages=%d retained_bytes=%d dropped_messages=%d capacity_drops=%d oversize_drops=%d closed_drops=%d invalid_drops=%d connect_failures=%d push_failures=%d", state, s.Messages, s.Bytes, s.DroppedMessages, s.CapacityDrops, s.OversizeDrops, s.ClosedDrops, s.InvalidDrops, s.ConnectFailures, s.PushFailures)
}
