package main

import (
	"errors"
	"net"
)

type ProxyListener struct {
	addr      net.Addr
	closeChan chan struct{}
	connChan  chan net.Conn
}

func newProxyListener(addr net.Addr, connChan chan net.Conn) *ProxyListener {
	return &ProxyListener{
		addr:      addr,
		closeChan: make(chan struct{}),
		connChan:  connChan,
	}
}

func (l *ProxyListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connChan:
		return conn, nil
	case <-l.closeChan:
		return nil, errors.New("listener closed")
	}
}

func (l *ProxyListener) Close() error {
	close(l.closeChan)
	return nil
}

func (l *ProxyListener) Addr() net.Addr {
	return l.addr
}
