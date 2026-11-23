package forwarderbs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"github.com/kudzutechnologies/analytics/api"
	"github.com/kudzutechnologies/analytics/client"
	log "github.com/sirupsen/logrus"
	"net/http"
	"os"
	"sync"
	"time"
)

type AnalyticsTransport interface {
	ConnectContext(context.Context) error
	PushMetricsOnce(context.Context, *api.AnalyticsMetrics) error
	Disconnect() error
}
type Dependencies struct {
	Persistence GatewayPersistence
	Analytics   AnalyticsTransport
	Logger      log.FieldLogger
}
type Forwarder struct {
	proxy       *WSProxy
	analytics   *AnalyticsForwarder
	config      ForwarderConfig
	once        sync.Once
	done        chan struct{}
	shutdownErr error
	debugFile   *os.File
	startOnce   sync.Once
}

func New(c ForwarderConfig, d Dependencies) (*Forwarder, error) {
	if d.Logger != nil {
		c.Logger = d.Logger
	}
	check := c
	if d.Analytics != nil {
		check.ClientId = "injected"
		check.ClientKey = "injected"
	}
	if len(c.ListenTLSCertPEM) > 0 {
		check.ListenTLSCert = "in-memory"
		check.ListenTLSKey = "in-memory"
	}
	if c.GatewayAuthorizations != nil {
		check.GatewayAuthFile = "in-memory"
	}
	if d.Persistence != nil {
		check.CertsDir = "in-memory"
	}
	if e := ValidateForwarderConfig(&check); e != nil {
		return nil, e
	}
	c.MaxWSStreams = check.MaxWSStreams
	var certStore *CertStore
	if d.Persistence == nil && c.CertsDir != "" {
		d.Persistence = &FilesystemPersistence{Dir: c.CertsDir}
	}
	if d.Persistence != nil {
		trust, e := material(c.CertsCACertPEM, c.CertsCACert)
		if e != nil {
			return nil, e
		}
		var ca *x509.Certificate
		if len(trust) > 0 {
			b, _ := pem.Decode(trust)
			if b == nil {
				return nil, fmt.Errorf("invalid CA PEM")
			}
			ca, e = x509.ParseCertificate(b.Bytes)
			if e != nil {
				return nil, e
			}
		}
		if fs, ok := d.Persistence.(*FilesystemPersistence); ok && fs.ReadOnly && c.CUPSMode == "managed" {
			return nil, ErrReadOnly
		}
		certStore = &CertStore{Persistence: d.Persistence, IssuerConfig: &c, writable: true, caCert: ca, certs: map[string]*tls.Certificate{}, serverCerts: map[string]*tls.Certificate{}, lnsConfigs: map[string]*GatewayLNSConfig{}, dataURIs: map[string]string{}}
	}
	proxyConfig := &WSProxyConfig{
		CUPSMaxCredentialBytes: c.CUPSMaxCredentialBytes,
		ProxyMode:              c.ProxyMode, PublicURI: c.PublicURI, GatewayAuthFile: c.GatewayAuthFile,
		LocalRouter: c.GatewayEUI, UpstreamAllowedOrigins: c.UpstreamAllowedOrigins, CUPSMode: c.CUPSMode,
		InjectRouterIntoVersion:  c.InjectRouterIntoVersion,
		ListenHost:               c.ListenHost,
		ListenPort:               c.ListenPort,
		ConnectURI:               c.ConnectURI,
		ConnectDiscoveryURI:      c.ConnectDiscoveryURI,
		ConnectDiscoveryPath:     c.ConnectDiscoveryPath,
		ConnectDataPath:          c.ConnectDataPath,
		ConnectDataURI:           c.ConnectDataURI,
		ConnectWSSubprotocol:     c.ConnectWSSubprotocol,
		ListenTLSCert:            c.ListenTLSCert,
		ListenTLSKey:             c.ListenTLSKey,
		ConnectTLSCA:             c.ConnectTLSCA,
		ConnectTLSInsecure:       c.ConnectTLSInsecure,
		ConnectDataTLSServerName: c.ConnectDataTLSServerName,
		ConnectDataTLSCA:         c.ConnectDataTLSCA,
		ConnectDataTLSInsecure:   c.ConnectDataTLSInsecure,
		MaxStreams:               c.MaxWSStreams,
		Events:                   nil,
		DebugDump:                nil,
		CertStore:                certStore,
		CertsUpstreamClientCert:  c.CertsUpstreamClientCert,
		Verbose:                  c.Verbose,
	}
	proxyConfig.Logger = c.Logger
	proxyConfig.GatewayAuthorizations = c.GatewayAuthorizations
	proxyConfig.UpstreamCAPEM = c.ConnectTLSCAPEM
	proxyConfig.DataCAPEM = c.ConnectDataTLSCAPEM
	if len(c.ListenTLSCertPEM) > 0 {
		pair, e := tls.X509KeyPair(c.ListenTLSCertPEM, c.ListenTLSKeyPEM)
		if e != nil {
			return nil, e
		}
		proxyConfig.ListenCertificate = &pair
	}
	var debugFile *os.File
	if c.DebugDump != "" && c.DebugDumpMaxBytes > 0 {
		var e error
		debugFile, e = os.OpenFile(c.DebugDump, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if e != nil {
			return nil, e
		}
		st, e := debugFile.Stat()
		if e != nil || !st.Mode().IsRegular() {
			debugFile.Close()
			return nil, fmt.Errorf("debug dump requires regular file")
		}
		if e = debugFile.Chmod(0600); e != nil {
			debugFile.Close()
			return nil, e
		}
		proxyConfig.DebugDump = NewDebugDumpWriter(debugFile, int64(c.DebugDumpMaxBytes)-st.Size())
	}
	proxy, e := NewWSProxy(proxyConfig)
	if e != nil {
		return nil, e
	}
	a := CreateAnalyticsForwarder(c, nil, proxy)
	a.backend = d.Analytics
	if a.backend == nil {
		a.backend = client.CreateAnalyticsClient(client.AnalyticsClientConfig{ClientId: c.ClientId, ClientKey: c.ClientKey, Endpoint: c.Endpoint, CAFile: c.CAFile, ServerSide: &c.ServerSide, ConnectTimeout: int32(c.ConnectTimeout), RequestTimeout: int32(c.RequestTimeout), MaxReconnectBackoff: int32(c.MaxReconnectBackoff)})
	}
	if certStore != nil {
		certStore.operationContext = proxy.root
	}
	proxy.SetEvents(a)
	return &Forwarder{proxy: proxy, analytics: a, config: c, done: make(chan struct{}), debugFile: debugFile}, nil
}
func (f *Forwarder) Handler() http.Handler  { return f.proxy.Handler() }
func (f *Forwarder) TLSConfig() *tls.Config { return f.proxy.TLSConfig() }
func (f *Forwarder) Start(ctx context.Context) error {
	select {
	case <-f.done:
		return fmt.Errorf("forwarder stopped")
	default:
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	f.startOnce.Do(func() {
		f.analytics.Start()
		go func() {
			select {
			case <-ctx.Done():
				c, cancel := context.WithTimeout(context.Background(), time.Duration(f.config.ShutdownDeadlineMS)*time.Millisecond)
				defer cancel()
				f.Shutdown(c)
			case <-f.done:
			}
		}()
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-f.done:
					return
				case <-ticker.C:
					f.logDeliveryStatus("running")
					f.proxy.LogSessionStatus()
				}
			}
		}()
	})
	return nil
}
func (f *Forwarder) Shutdown(ctx context.Context) error {
	f.once.Do(func() {
		defer close(f.done)
		if f.debugFile != nil {
			defer f.debugFile.Close()
		}
		f.shutdownErr = f.proxy.Shutdown(ctx)
		if !f.analytics.Stop(ctx) && f.shutdownErr == nil {
			f.shutdownErr = fmt.Errorf("analytics drain incomplete")
		}
		f.logDeliveryStatus("stopped")
	})
	return f.shutdownErr
}
func (f *Forwarder) Run(ctx context.Context) error {
	if e := f.Start(ctx); e != nil {
		return e
	}
	ch := make(chan error, 1)
	go func() { ch <- f.proxy.Start() }()
	var e error
	select {
	case <-ctx.Done():
	case e = <-ch:
	}
	c, cancel := context.WithTimeout(context.Background(), time.Duration(f.config.ShutdownDeadlineMS)*time.Millisecond)
	defer cancel()
	se := f.Shutdown(c)
	if e != nil && e != http.ErrServerClosed {
		return e
	}
	return se
}

func (f *Forwarder) DeliveryStats() DeliveryStats { return f.analytics.DeliveryStats() }

func instanceLogger(l log.FieldLogger) log.FieldLogger {
	if l == nil {
		return log.StandardLogger()
	}
	return l
}

func (f *Forwarder) logDeliveryStatus(state string) {
	s := f.analytics.DeliveryStats()
	instanceLogger(f.config.Logger).Infof("%s retained_messages=%d retained_bytes=%d dropped_messages=%d capacity_drops=%d oversize_drops=%d closed_drops=%d invalid_drops=%d connect_failures=%d push_failures=%d", state, s.Messages, s.Bytes, s.DroppedMessages, s.CapacityDrops, s.OversizeDrops, s.ClosedDrops, s.InvalidDrops, s.ConnectFailures, s.PushFailures)
}
