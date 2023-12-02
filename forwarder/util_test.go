package main

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
)

///////////////////////////////////////

type UDPSock struct {
	local   *net.UDPAddr
	remote  *net.UDPAddr
	last    *net.UDPAddr
	conn    *net.UDPConn
	timeout time.Duration
	closed  bool
	t       *testing.T
}

func CreateSocket(t *testing.T) *UDPSock {
	for {
		pBase := 30500 + (rand.Intn(3500))*10
		lUdp, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", pBase))
		if err != nil {
			t.Errorf("Could not resolve addr: %s", err.Error())
			t.FailNow()
			return nil
		}
		rUdp, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", pBase+1))
		if err != nil {
			t.Errorf("Could not resolve addr: %s", err.Error())
			t.FailNow()
			return nil
		}

		conn, err := net.ListenUDP("udp", lUdp)
		if err != nil {
			if isErrorAddressAlreadyInUse(err) {
				continue
			}
			t.Errorf("Could not listen: %s", err.Error())
			t.FailNow()
			return nil
		}

		return &UDPSock{
			t:       t,
			timeout: time.Millisecond * 100,
			local:   lUdp,
			remote:  rUdp,
			conn:    conn,
		}
	}
}

func CreateSocketToRemote(t *testing.T, toSockeet *UDPSock) *UDPSock {
	for {
		pBase := 30500 + (rand.Intn(17500))*2
		lUdp, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", pBase))
		if err != nil {
			t.Errorf("Could not resolve addr: %s", err.Error())
			t.FailNow()
			return nil
		}

		conn, err := net.ListenUDP("udp", lUdp)
		if err != nil {
			if isErrorAddressAlreadyInUse(err) {
				continue
			}
			t.Errorf("Could not listen: %s", err.Error())
			t.FailNow()
			return nil
		}
		return &UDPSock{
			t:       t,
			timeout: time.Millisecond * 100,
			local:   lUdp,
			remote:  toSockeet.local,
			conn:    conn,
		}
	}
}

func CreateSocketWithSameRemote(t *testing.T, toSockeet *UDPSock) *UDPSock {
	for {
		pBase := 30500 + (rand.Intn(17500))*2
		lUdp, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", pBase))
		if err != nil {
			t.Errorf("Could not resolve addr: %s", err.Error())
			t.FailNow()
			return nil
		}

		conn, err := net.ListenUDP("udp", lUdp)
		if err != nil {
			if isErrorAddressAlreadyInUse(err) {
				continue
			}
			t.Errorf("Could not listen: %s", err.Error())
			t.FailNow()
			return nil
		}
		return &UDPSock{
			t:       t,
			timeout: time.Millisecond * 100,
			local:   lUdp,
			remote:  toSockeet.remote,
			conn:    conn,
		}
	}
}

func isErrorAddressAlreadyInUse(err error) bool {
	errOpError, ok := err.(*net.OpError)
	if !ok {
		return false
	}
	errSyscallError, ok := errOpError.Err.(*os.SyscallError)
	if !ok {
		return false
	}
	errErrno, ok := errSyscallError.Err.(syscall.Errno)
	if !ok {
		return false
	}
	if errErrno == syscall.EADDRINUSE {
		return true
	}
	const WSAEADDRINUSE = 10048
	if runtime.GOOS == "windows" && errErrno == WSAEADDRINUSE {
		return true
	}
	return false
}

func (r *UDPSock) SetTimeout(timeout time.Duration) {
	r.timeout = timeout
}

func (r *UDPSock) Close() {
	r.closed = true
}

func (r *UDPSock) Restart() {
	r.conn.Close()
	conn, err := net.ListenUDP("udp", r.local)
	if err != nil {
		r.t.Errorf("Could not listen: %s", err.Error())
		r.t.FailNow()
		return
	}

	r.conn = conn
}

func (r *UDPSock) Read(bytes int) []byte {
	var buf [1024]byte
	r.conn.SetReadDeadline(time.Now().Add(r.timeout))
	l, recvAddr, err := r.conn.ReadFromUDP(buf[0:])
	if r.closed {
		return nil
	}
	if err != nil {
		r.t.Errorf("Could not read from %s: %s", r.local.String(), err.Error())
		r.t.FailNow()
		return nil
	}
	r.last = recvAddr
	return buf[0:l]
}

func (r *UDPSock) ReadWithAddr(bytes int) ([]byte, *net.UDPAddr) {
	var buf [1024]byte
	r.conn.SetReadDeadline(time.Now().Add(r.timeout))
	l, recvAddr, err := r.conn.ReadFromUDP(buf[0:])
	if r.closed {
		return nil, nil
	}
	if err != nil {
		r.t.Errorf("Could not read from %s: %s", r.local.String(), err.Error())
		r.t.FailNow()
		return nil, nil
	}
	r.last = recvAddr
	return buf[0:l], recvAddr
}

func (r *UDPSock) Send(bytes []byte) {
	_, err := r.conn.WriteToUDP(bytes[0:], r.remote)
	if r.closed {
		return
	}
	if err != nil {
		r.t.Errorf("Could not send to %s: %s", r.remote.String(), err.Error())
		r.t.FailNow()
	}
}

func (r *UDPSock) SendToAddr(addr *net.UDPAddr, bytes []byte) {
	_, err := r.conn.WriteToUDP(bytes[0:], addr)
	if r.closed {
		return
	}
	if err != nil {
		r.t.Errorf("Could not send to %s: %s", addr.String(), err.Error())
		r.t.FailNow()
	}
}

func (r *UDPSock) Reply(bytes []byte) {
	if r.closed {
		return
	}
	if r.last == nil {
		r.t.Errorf("Could reply to any address because no message was received")
		r.t.FailNow()
		return
	}
	_, err := r.conn.WriteToUDP(bytes[0:], r.last)
	if err != nil {
		r.t.Errorf("Could not send to %s: %s", r.last.String(), err.Error())
		r.t.FailNow()
	}
}
