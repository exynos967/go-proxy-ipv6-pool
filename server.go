package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"time"
)

const (
	inboundTimeout          = 30 * time.Second
	temporaryAcceptBackoff  = 5 * time.Millisecond
	maxTemporaryAcceptDelay = time.Second
)

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: inboundTimeout,
		IdleTimeout:       inboundTimeout,
	}
}

func listenAndServeSocks5(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	return serveSocks5Listener(listener)
}

func serveSocks5Listener(listener net.Listener) error {
	var delay time.Duration
	for {
		conn, err := listener.Accept()
		if err != nil {
			if isTemporaryNetError(err) {
				delay = nextTemporaryAcceptDelay(delay)
				log.Printf("[socks5] temporary accept error: %v; retrying in %s", err, delay)
				time.Sleep(delay)
				continue
			}
			return err
		}

		delay = 0
		go func(clientConn net.Conn) {
			_ = socks5Server.ServeConn(newSocks5HandshakeDeadlineConn(clientConn, inboundTimeout))
		}(conn)
	}
}

func nextTemporaryAcceptDelay(previous time.Duration) time.Duration {
	if previous <= 0 {
		return temporaryAcceptBackoff
	}

	next := previous * 2
	if next > maxTemporaryAcceptDelay {
		return maxTemporaryAcceptDelay
	}
	return next
}

func isTemporaryNetError(err error) bool {
	var netErr interface{ Temporary() bool }
	return errors.As(err, &netErr) && netErr.Temporary()
}

type socks5HandshakeDeadlineConn struct {
	net.Conn
	deadlineCleared bool
}

func newSocks5HandshakeDeadlineConn(conn net.Conn, timeout time.Duration) net.Conn {
	_ = conn.SetDeadline(time.Now().Add(timeout))
	return &socks5HandshakeDeadlineConn{Conn: conn}
}

func (conn *socks5HandshakeDeadlineConn) Write(data []byte) (int, error) {
	written, err := conn.Conn.Write(data)
	if err == nil && !conn.deadlineCleared && isSocks5ConnectSuccessReply(data[:written]) {
		conn.deadlineCleared = true
		_ = conn.Conn.SetDeadline(time.Time{})
	}
	return written, err
}

func isSocks5ConnectSuccessReply(data []byte) bool {
	return len(data) >= 4 && data[0] == 0x05 && data[1] == 0x00
}
