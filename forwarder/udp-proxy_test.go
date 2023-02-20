package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"net"
	"testing"
	"time"
)

type WriteFn = func([]byte) error

type TestSockets struct {
	PacketForwarderPort           int
	PacketForwarderSock           *net.UDPConn
	PacketForwarderLastRemoteAddr *net.UDPAddr

	ServerPort           int
	ServerSock           *net.UDPConn
	ServerLastRemoteAddr *net.UDPAddr

	t *testing.T
}

func CreateTestSockets(t *testing.T) *TestSockets {
	sock := &TestSockets{}

	// Create 4 random ports
	pBase := 30500 + rand.Intn(35000)
	sock.t = t
	sock.PacketForwarderPort = pBase
	sock.ServerPort = pBase + 1

	return sock
}

func (s *TestSockets) ForwarderDrain() {
	err := s.PacketForwarderSock.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	if err != nil {
		s.t.Fatalf("Error setting deadline: %s", err.Error())
	}

	buf := make([]byte, 1500)
	for {
		n, _, _ := s.PacketForwarderSock.ReadFromUDP(buf)
		if n == 0 {
			break
		}
	}

	err = s.PacketForwarderSock.SetReadDeadline(time.Now().Add(1 * time.Second))
	if err != nil {
		s.t.Fatalf("Error setting deadline: %s", err.Error())
	}
}

func (s *TestSockets) ForwarderClose() {
	s.PacketForwarderSock.Close()
}

func (s *TestSockets) ForwarderConnect() {
	// Create a server socket
	aFwd, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("127.0.0.1:%d", s.PacketForwarderPort))
	if err != nil {
		s.t.Fatalf("Could not connect forwarder: address error: %s", err.Error())
	}
	s.PacketForwarderSock, err = net.DialUDP("udp4", nil, aFwd)
	if err != nil {
		s.t.Fatalf("Could not connect forwarder: %s", err.Error())
	}
}

func (s *TestSockets) ForwarderSend(buf []byte) {
	n, err := s.PacketForwarderSock.Write(buf)
	if n != len(buf) {
		s.t.Fatalf("Did not write to forwarder: %d != %d", n, len(buf))
	}
	if err != nil {
		s.t.Fatalf("Could write to forwarder: %s", err.Error())
	}
}

func (s *TestSockets) ForwarderRecv() []byte {
	buf := make([]byte, 1500)
	n, addr, err := s.PacketForwarderSock.ReadFromUDP(buf)
	if err != nil {
		s.t.Fatalf("Could read from forwarder: %s", err.Error())
	}
	s.PacketForwarderLastRemoteAddr = addr
	return buf[0:n]
}

func (s *TestSockets) ServerListen() {
	// Create a server socket
	aServ, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("127.0.0.1:%d", s.ServerPort))
	if err != nil {
		s.t.Fatalf("Could not listen server: address error: %s", err.Error())
	}
	s.ServerSock, err = net.ListenUDP("udp4", aServ)
	if err != nil {
		s.t.Fatalf("Could not listen server: %s", err.Error())
	}
}

func (s *TestSockets) ServerDrain() {
	err := s.ServerSock.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	if err != nil {
		s.t.Fatalf("Error setting deadline: %s", err.Error())
	}

	buf := make([]byte, 1500)
	for {
		n, _, _ := s.ServerSock.ReadFromUDP(buf)
		if n == 0 {
			break
		}
	}

	err = s.ServerSock.SetReadDeadline(time.Now().Add(1 * time.Second))
	if err != nil {
		s.t.Fatalf("Error setting deadline: %s", err.Error())
	}
}

func (s *TestSockets) ServerClose() {
	s.ServerSock.Close()
}

func (s *TestSockets) ServerSend(buf []byte) {
	n, err := s.ServerSock.WriteToUDP(buf, s.ServerLastRemoteAddr)
	if n != len(buf) {
		s.t.Fatalf("Did not write to server: %d != %d", n, len(buf))
	}
	if err != nil {
		s.t.Fatalf("Could write to server: %s", err.Error())
	}
}

func (s *TestSockets) ServerRecv() []byte {
	buf := make([]byte, 1500)
	n, addr, err := s.ServerSock.ReadFromUDP(buf)
	if err != nil {
		s.t.Fatalf("Could read from server: %s", err.Error())
	}
	s.ServerLastRemoteAddr = addr
	return buf[0:n]
}

func TestPacketForwarding(t *testing.T) {
	s := CreateTestSockets(t)

	_, err := CreateUDPProxy(ForwarderConfig{
		LocalAddress:  fmt.Sprintf("127.0.0.1:%d", s.PacketForwarderPort),
		RemoteAddress: fmt.Sprintf("127.0.0.1:%d", s.ServerPort),
		BufferSize:    1500,
	})
	if err != nil {
		t.Fatalf("Could not create UDP proxy: %s", err.Error())
	}

	// Connect
	s.ForwarderConnect()
	s.ServerListen()

	// Test exchanging a few random packets in one direction
	rBuf := make([]byte, 1024)
	for i := 0; i < 100; i++ {
		rand.Read(rBuf)
		sz := rand.Intn(1000) + 24

		buf1tx := rBuf[0:sz]
		s.ForwarderSend(buf1tx)
		buf1rx := s.ServerRecv()
		if !bytes.Equal(buf1tx, buf1rx) {
			t.Fail()
		}
	}

	// Test exchanging a few random packets in the other direction
	for i := 0; i < 100; i++ {
		rand.Read(rBuf)
		sz := rand.Intn(1000) + 24

		buf1tx := rBuf[0:sz]
		s.ServerSend(buf1tx)
		buf1rx := s.ForwarderRecv()
		if !bytes.Equal(buf1tx, buf1rx) {
			t.Fail()
		}
	}

	// Mingle traffic
	for i := 0; i < 100; i++ {
		rand.Read(rBuf)
		sz := rand.Intn(1000) + 24

		buf1tx := rBuf[0:sz]
		s.ServerSend(buf1tx)
		buf1rx := s.ForwarderRecv()
		if !bytes.Equal(buf1tx, buf1rx) {
			t.Fail()
		}

		s.ForwarderSend(buf1tx)
		buf1rx = s.ServerRecv()
		if !bytes.Equal(buf1tx, buf1rx) {
			t.Fail()
		}
	}

	// Random direction at a time
	for i := 0; i < 100; i++ {
		rand.Read(rBuf)
		sz := rand.Intn(1000) + 24
		buf1tx := rBuf[0:sz]

		dir := rand.Intn(10)
		if dir < 5 {
			s.ServerSend(buf1tx)
			buf1rx := s.ForwarderRecv()
			if !bytes.Equal(buf1tx, buf1rx) {
				t.Fail()
			}
		} else {
			s.ForwarderSend(buf1tx)
			buf1rx := s.ServerRecv()
			if !bytes.Equal(buf1tx, buf1rx) {
				t.Fail()
			}
		}
	}

}

func TestServerReconnect(t *testing.T) {
	s := CreateTestSockets(t)

	_, err := CreateUDPProxy(ForwarderConfig{
		LocalAddress:  fmt.Sprintf("127.0.0.1:%d", s.PacketForwarderPort),
		RemoteAddress: fmt.Sprintf("127.0.0.1:%d", s.ServerPort),
		BufferSize:    1500,
	})
	if err != nil {
		t.Fatalf("Could not create UDP proxy: %s", err.Error())
	}

	// Connect only forwarder
	s.ForwarderConnect()

	// Send a few data on a dead end
	rBuf := make([]byte, 1024)
	for i := 0; i < 100; i++ {
		rand.Read(rBuf)
		sz := rand.Intn(1000) + 24

		buf1tx := rBuf[0:sz]
		s.ForwarderSend(buf1tx)
	}

	// Test a few connect/disconnect cycles
	for c := 0; c < 50; c++ {
		// Connect now
		s.ServerListen()
		s.ServerDrain()

		// Now receiving should work
		for i := 0; i < 100; i++ {
			rand.Read(rBuf)
			sz := rand.Intn(1000) + 24

			buf1tx := rBuf[0:sz]
			s.ForwarderSend(buf1tx)
			buf1rx := s.ServerRecv()
			if !bytes.Equal(buf1tx, buf1rx) {
				t.Fatalf("Mismatching Tx and Rx buffers")
			}

			// Also check other direction
			rand.Read(rBuf)
			sz = rand.Intn(1000) + 24
			s.ServerSend(buf1tx)
			buf1rx = s.ForwarderRecv()
			if !bytes.Equal(buf1tx, buf1rx) {
				t.Fatalf("Mismatching Tx and Rx buffers")
			}
		}

		// Disconnect
		s.ServerClose()
	}
}

func TestClientReconnect(t *testing.T) {
	s := CreateTestSockets(t)

	_, err := CreateUDPProxy(ForwarderConfig{
		LocalAddress:  fmt.Sprintf("127.0.0.1:%d", s.PacketForwarderPort),
		RemoteAddress: fmt.Sprintf("127.0.0.1:%d", s.ServerPort),
		BufferSize:    1500,
	})
	if err != nil {
		t.Fatalf("Could not create UDP proxy: %s", err.Error())
	}

	// Connect only server
	rBuf := make([]byte, 1024)
	s.ServerListen()

	// Test a few connect/disconnect cycles
	for c := 0; c < 50; c++ {
		// Connect now
		s.ForwarderConnect()
		s.ForwarderDrain()

		// Now receiving should work
		for i := 0; i < 100; i++ {
			rand.Read(rBuf)
			sz := rand.Intn(1000) + 24

			buf1tx := rBuf[0:sz]
			s.ForwarderSend(buf1tx)
			buf1rx := s.ServerRecv()
			if !bytes.Equal(buf1tx, buf1rx) {
				t.Fatalf("Mismatching Tx and Rx buffers")
			}

			// Also check other direction
			rand.Read(rBuf)
			sz = rand.Intn(1000) + 24
			s.ServerSend(buf1tx)
			buf1rx = s.ForwarderRecv()
			if !bytes.Equal(buf1tx, buf1rx) {
				t.Fatalf("Mismatching Tx and Rx buffers")
			}
		}

		// Disconnect
		s.ForwarderClose()
	}
}
