package main

import (
	"bytes"
	"net"
	"testing"
	"time"
	// log "github.com/sirupsen/logrus"
)

type BufHandler struct {
	LastUpLocal  []byte
	lastUpRemote []byte
	LastDnLocal  []byte
	LastDnRemote []byte
}

func (b *BufHandler) UpLocalData(data []byte, localEp *net.UDPAddr) {
	tmp := make([]byte, len(data))
	copy(tmp, data)
	b.LastUpLocal = tmp
}
func (b *BufHandler) UpRemoteData(data []byte, localEp *net.UDPAddr) {
	tmp := make([]byte, len(data))
	copy(tmp, data)
	b.lastUpRemote = tmp
}
func (b *BufHandler) DnLocalData(data []byte, localEp *net.UDPAddr) {
	tmp := make([]byte, len(data))
	copy(tmp, data)
	b.LastDnLocal = tmp
}
func (b *BufHandler) DnRemoteData(data []byte, localEp *net.UDPAddr) {
	tmp := make([]byte, len(data))
	copy(tmp, data)
	b.LastDnRemote = tmp
}

func TestEventsOfUDPProxy(t *testing.T) {
	// log.SetLevel(log.DebugLevel)

	clientUp := CreateSocket(t)
	clientDn := CreateSocket(t)
	serverUp := CreateSocket(t)
	serverDn := CreateSocket(t)

	bufs := &BufHandler{}
	proxy, _ := CreateUDPProxy(&UDPProxyConfig{
		UpListenAddr:        clientUp.remote,
		UpConnectAddr:       serverUp.local,
		UpConnectBindAddr:   nil,
		DownListenAddr:      clientDn.remote,
		DownConnectAddr:     serverDn.local,
		DownConnectBindAddr: nil,
		BufferSize:          1024,
		SocketStreams:       16,
		ReconnectInterval:   1,
		Events:              bufs,
	})

	// Local Up
	bufLocalUp := randBuf(1024)
	clientUp.Send(bufLocalUp)
	expectToReceive(t, serverUp, bufLocalUp)
	if !bytes.Equal(bufLocalUp, bufs.LastUpLocal) {
		t.Fail()
	}

	// Remote Up
	bufRemoteUp := randBuf(1024)
	serverUp.Reply(bufRemoteUp)
	expectToReceive(t, clientUp, bufRemoteUp)
	if !bytes.Equal(bufRemoteUp, bufs.lastUpRemote) {
		t.Fail()
	}

	// Local Down
	bufLocalDn := randBuf(1024)
	clientDn.Send(bufLocalDn)
	expectToReceive(t, serverDn, bufLocalDn)
	if !bytes.Equal(bufLocalDn, bufs.LastDnLocal) {
		t.Fail()
	}

	// Remote Down
	bufRemoteDn := randBuf(1024)
	serverDn.Reply(bufRemoteDn)
	expectToReceive(t, clientDn, bufRemoteDn)
	if !bytes.Equal(bufRemoteDn, bufs.LastDnRemote) {
		t.Fail()
	}

	proxy.Close()
}

func TestSingleSocketProxy(t *testing.T) {
	client1 := CreateSocket(t)
	server := CreateSocket(t)

	proxy, _ := CreateUDPProxy(&UDPProxyConfig{
		UpListenAddr:        client1.remote,
		UpConnectAddr:       server.local,
		UpConnectBindAddr:   nil,
		DownListenAddr:      nil,
		DownConnectAddr:     nil,
		DownConnectBindAddr: nil,
		BufferSize:          1024,
		SocketStreams:       16,
		ReconnectInterval:   1,
		Events:              nil,
	})

	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		client1.Send(buf)
		expectToReceive(t, server, buf)
	}

	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		server.Reply(buf)
		expectToReceive(t, client1, buf)
	}

	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		client1.Send(buf)
		expectToReceive(t, server, buf)

		buf = randBuf(1024)
		server.Reply(buf)
		expectToReceive(t, client1, buf)
	}

	proxy.Close()
}

func TestMultipleClientsProxy(t *testing.T) {
	client1 := CreateSocket(t)
	server := CreateSocket(t)

	proxy, _ := CreateUDPProxy(&UDPProxyConfig{
		UpListenAddr:        client1.remote,
		UpConnectAddr:       server.local,
		UpConnectBindAddr:   nil,
		DownListenAddr:      nil,
		DownConnectAddr:     nil,
		DownConnectBindAddr: nil,
		BufferSize:          1024,
		SocketStreams:       100,
		ReconnectInterval:   1,
		Events:              nil,
	})

	// Send data from a few clients
	var client []*UDPSock
	var clientAddr []*net.UDPAddr
	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		c := CreateSocketWithSameRemote(t, client1)

		c.Send(buf)
		ca := expectToReceive(t, server, buf)

		client = append(client, c)
		clientAddr = append(clientAddr, ca)
	}

	// Send data back from the server to the clients
	for i := 0; i < 100; i++ {
		c := client[i]
		ca := clientAddr[i]
		buf := randBuf(1024)
		server.SendToAddr(ca, buf)
		expectToReceive(t, c, buf)
	}

	proxy.Close()
}

func TestMultipleClientsMultiPortProxy(t *testing.T) {
	clientUp := CreateSocket(t)
	clientDn := CreateSocket(t)
	serverUp := CreateSocket(t)
	serverDn := CreateSocket(t)

	proxy, _ := CreateUDPProxy(&UDPProxyConfig{
		UpListenAddr:        clientUp.remote,
		UpConnectAddr:       serverUp.local,
		UpConnectBindAddr:   nil,
		DownListenAddr:      clientDn.remote,
		DownConnectAddr:     serverDn.local,
		DownConnectBindAddr: nil,
		BufferSize:          1024,
		SocketStreams:       100,
		ReconnectInterval:   1,
		Events:              nil,
	})

	// Send data from a few clients
	var clientsUp []*UDPSock
	var clientsUpAddr []*net.UDPAddr
	var clientsDn []*UDPSock
	var clientsDnAddr []*net.UDPAddr

	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		c := CreateSocketWithSameRemote(t, clientUp)
		c.Send(buf)
		ca := expectToReceive(t, serverUp, buf)
		clientsUp = append(clientsUp, c)
		clientsUpAddr = append(clientsUpAddr, ca)

		buf = randBuf(1024)
		c = CreateSocketWithSameRemote(t, clientDn)
		c.Send(buf)
		ca = expectToReceive(t, serverDn, buf)
		clientsDn = append(clientsDn, c)
		clientsDnAddr = append(clientsDnAddr, ca)
	}

	// Send data back from the server to the clients
	for i := 0; i < 100; i++ {
		c := clientsUp[i]
		ca := clientsUpAddr[i]
		buf := randBuf(1024)
		serverUp.SendToAddr(ca, buf)
		expectToReceive(t, c, buf)

		c = clientsDn[i]
		ca = clientsDnAddr[i]
		buf = randBuf(1024)
		serverDn.SendToAddr(ca, buf)
		expectToReceive(t, c, buf)
	}

	proxy.Close()
}

func TestRemoteReconnect(t *testing.T) {
	client1 := CreateSocket(t)
	server := CreateSocket(t)

	proxy, _ := CreateUDPProxy(&UDPProxyConfig{
		UpListenAddr:        client1.remote,
		UpConnectAddr:       server.local,
		UpConnectBindAddr:   nil,
		DownListenAddr:      nil,
		DownConnectAddr:     nil,
		DownConnectBindAddr: nil,
		BufferSize:          1024,
		SocketStreams:       16,
		ReconnectInterval:   1,
		Events:              nil,
	})

	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		client1.Send(buf)
		expectToReceive(t, server, buf)
	}

	server.Restart()

	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		client1.Send(buf)
		expectToReceive(t, server, buf)
	}

	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		server.Reply(buf)
		expectToReceive(t, client1, buf)
	}

	proxy.Close()
}

func TestLocalDisconnect(t *testing.T) {
	client1 := CreateSocket(t)
	server := CreateSocket(t)
	// log.SetLevel(log.DebugLevel)

	proxy, _ := CreateUDPProxy(&UDPProxyConfig{
		UpListenAddr:        client1.remote,
		UpConnectAddr:       server.local,
		UpConnectBindAddr:   nil,
		DownListenAddr:      nil,
		DownConnectAddr:     nil,
		DownConnectBindAddr: nil,
		BufferSize:          1024,
		SocketStreams:       16,
		ReconnectInterval:   1,
		Events:              nil,
	})

	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		client1.Send(buf)
		expectToReceive(t, server, buf)
	}

	proxy.upSock.Close()
	server.Restart()

	time.Sleep(time.Millisecond * 1100)

	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		client1.Send(buf)
		expectToReceive(t, server, buf)
	}

	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		server.Reply(buf)
		expectToReceive(t, client1, buf)
	}

	proxy.Close()
}
