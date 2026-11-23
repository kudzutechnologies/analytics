package main

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func waitForShutdown(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not complete")
	}
}

func TestProxyStreamConcurrentCloseDuringReceive(t *testing.T) {
	upstream := CreateSocket(t)
	local := CreateSocket(t)
	stream := CreateProxyStream(&ProxyStreamConfig{
		Name: "concurrent-close", BufferSize: 1024, Local: local.conn,
		LocalReplyAddress: local.remote, RemoteAddress: upstream.local,
		Events: ProxyStreamEvents{RemoteError: func(error) {}, LocalError: func(error) {}, DataReceived: func([]byte, *net.UDPAddr) {}},
	})
	if err := stream.HandleLocalData([]byte("request")); err != nil {
		t.Fatal(err)
	}
	expectToReceive(t, upstream, []byte("request"))
	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); stream.Close() }()
		}
		wg.Wait()
		close(done)
	}()
	waitForShutdown(t, done)
	if err := stream.HandleLocalData([]byte("after-close")); !errors.Is(err, ErrSocketClosed) {
		t.Fatalf("send after close: %v", err)
	}
}

func TestProxyStreamReadFailureDoesNotWaitForItself(t *testing.T) {
	upstream := CreateSocket(t)
	local := CreateSocket(t)
	failed := make(chan struct{})
	stream := CreateProxyStream(&ProxyStreamConfig{
		Name: "read-error", BufferSize: 1024, Local: local.conn,
		LocalReplyAddress: local.remote, RemoteAddress: upstream.local,
		Events: ProxyStreamEvents{RemoteError: func(error) { close(failed) }, LocalError: func(error) {}, DataReceived: func([]byte, *net.UDPAddr) {}},
	})
	t.Cleanup(stream.Close)
	if err := stream.HandleLocalData([]byte("request")); err != nil {
		t.Fatal(err)
	}
	expectToReceive(t, upstream, []byte("request"))
	stream.mu.Lock()
	remote := stream.remote
	stream.mu.Unlock()
	remote.Close() // Force a reader error without marking the stream intentionally closed.
	waitForShutdown(t, failed)
	done := make(chan struct{})
	go func() { stream.Close(); close(done) }()
	waitForShutdown(t, done)
}

func TestUDPProxyCloseCancelsRestart(t *testing.T) {
	upstream := CreateSocket(t)
	proxy, err := CreateUDPProxy(&UDPProxyConfig{
		UpListenAddr:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)},
		UpConnectAddr: upstream.local, BufferSize: 1024, SocketStreams: 16,
		ReconnectInterval: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proxy.Close)
	address := proxy.upSock.LocalAddr().(*net.UDPAddr)
	proxy.scheduleRestart()
	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); proxy.Close() }()
		}
		wg.Wait()
		close(done)
	}()
	waitForShutdown(t, done)
	// Close joined the retry worker and released the listening port.
	rebound, err := net.ListenUDP("udp", address)
	if err != nil {
		t.Fatalf("listener remains bound after close: %v", err)
	}
	rebound.Close()
}

func TestUDPProxyFailedDownBindReleasesUpListener(t *testing.T) {
	upstream := CreateSocket(t)
	occupied := CreateSocket(t)
	probe := CreateSocket(t)
	address := probe.local
	probe.Close()
	_, err := CreateUDPProxy(&UDPProxyConfig{
		UpListenAddr: address, UpConnectAddr: upstream.local,
		DownListenAddr: occupied.local, DownConnectAddr: upstream.local,
		BufferSize: 1024, SocketStreams: 16,
	})
	if err == nil {
		t.Fatal("expected occupied downlink port to fail")
	}
	rebound, err := net.ListenUDP("udp", address)
	if err != nil {
		t.Fatalf("partial bind leaked uplink socket: %v", err)
	}
	rebound.Close()
}
