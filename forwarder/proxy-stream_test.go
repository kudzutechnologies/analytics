package main

import (
	"bytes"
	"math/rand"
	"net"
	"testing"
)

type serverStream struct {
	t         *testing.T
	bind      *UDPSock
	remote    *net.UDPAddr
	lookup    map[string]*ProxyStream
	closed    bool
	closeWait chan bool
}

func (s *serverStream) createClient() *UDPSock {
	sIn := CreateSocketToRemote(s.t, s.bind)
	stream := CreateProxyStream(&ProxyStreamConfig{
		Name:              "stream",
		BufferSize:        1024,
		Local:             s.bind.conn,
		LocalReplyAddress: sIn.local,
		RemoteAddress:     s.remote,
		RemoteBindAddress: nil,
		Events: ProxyStreamEvents{
			LocalError:   func(err error) {},
			RemoteError:  func(err error) {},
			DataReceived: func(data []byte, rxFrom *net.UDPAddr) {},
		},
	})

	s.lookup[sIn.local.String()] = stream
	return sIn
}

func (s *serverStream) readThread() {
	for !s.closed {
		buf, addr := s.bind.ReadWithAddr(1024)
		if s.closed {
			break
		}
		if found, ok := s.lookup[addr.String()]; ok {
			found.HandleLocalData(buf)
		}
	}
	s.closeWait <- true
}

func (s *serverStream) close() {
	s.closed = true
	s.bind.Close()
	<-s.closeWait
}

func createServer(t *testing.T, remote *UDPSock) *serverStream {
	sBind := CreateSocket(t)
	stream := &serverStream{
		t:         t,
		bind:      sBind,
		remote:    remote.local,
		lookup:    make(map[string]*ProxyStream),
		closeWait: make(chan bool),
	}
	go stream.readThread()
	return stream
}

func randBuf(upToSz int) []byte {
	sz := rand.Intn(upToSz-24) + 24
	var rBuf []byte = make([]byte, sz)
	rand.Read(rBuf)
	return rBuf
}

func expectToReceive(t *testing.T, sock *UDPSock, data []byte) *net.UDPAddr {
	sBuf, addr := sock.ReadWithAddr(1024)
	if !bytes.Equal(data, sBuf) {
		t.Fail()
	}
	return addr
}

///////////////////////////////////////

func TestSingleClient(t *testing.T) {
	serverSocket := CreateSocket(t)
	server := createServer(t, serverSocket)

	// Test sending data from client to server
	client1 := server.createClient()
	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		client1.Send(buf)
		expectToReceive(t, serverSocket, buf)
	}

	// Test sending data from server to client
	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		serverSocket.Reply(buf)
		expectToReceive(t, client1, buf)
	}

	// Test interleaving data
	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		client1.Send(buf)
		expectToReceive(t, serverSocket, buf)

		buf = randBuf(1024)
		serverSocket.Reply(buf)
		expectToReceive(t, client1, buf)
	}

	server.close()
}

func TestMultipleClients(t *testing.T) {
	var c1, c2, c3 *net.UDPAddr

	serverSocket := CreateSocket(t)
	server := createServer(t, serverSocket)

	// Test sending data from client to server
	client1 := server.createClient()
	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		client1.Send(buf)
		c1 = expectToReceive(t, serverSocket, buf)
	}

	// Test sending data from another client to server
	client2 := server.createClient()
	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		client2.Send(buf)
		c2 = expectToReceive(t, serverSocket, buf)
	}

	// Test sending data from third client to server
	client3 := server.createClient()
	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		client3.Send(buf)
		c3 = expectToReceive(t, serverSocket, buf)
	}

	// Test sending data from server to client1
	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		serverSocket.SendToAddr(c1, buf)
		expectToReceive(t, client1, buf)
	}

	// Test sending data from server to client2
	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		serverSocket.SendToAddr(c2, buf)
		expectToReceive(t, client2, buf)
	}

	// Test sending data from server to client3
	for i := 0; i < 100; i++ {
		buf := randBuf(1024)
		serverSocket.SendToAddr(c3, buf)
		expectToReceive(t, client3, buf)
	}

	server.close()
}

func TestManyClients(t *testing.T) {
	var client []*UDPSock = nil
	var clientAddr []*net.UDPAddr = nil

	serverSocket := CreateSocket(t)
	server := createServer(t, serverSocket)

	for i := 0; i < 100; i++ {
		c := server.createClient()
		client = append(client, c)
		clientAddr = append(clientAddr, c.local)
	}

	for i := 0; i < 1000; i++ {
		buf := randBuf(1024)
		n := rand.Intn(100)
		c := client[n]
		ca := clientAddr[n]

		if rand.Intn(2) > 0 {
			// Client -> Server
			c.Send(buf)
			expectToReceive(t, serverSocket, buf)
		} else {
			// Server -> Client
			serverSocket.SendToAddr(ca, buf)
			expectToReceive(t, c, buf)
		}
	}

	server.close()
}
