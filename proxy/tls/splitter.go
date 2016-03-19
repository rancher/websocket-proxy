package tls

import (
	"bufio"
	"crypto/tls"
	"net"
)

type Conn struct {
	net.Conn
	bufReader *bufio.Reader
}

func (c *Conn) Read(b []byte) (int, error) {
	return c.bufReader.Read(b)
}

type SplitListener struct {
	net.Listener
	Config *tls.Config
}

func (l *SplitListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	newConn := &Conn{
		Conn:      c,
		bufReader: bufio.NewReader(c),
	}

	b, err := newConn.bufReader.Peek(1)
	if err != nil {
		c.Close()
		return nil, err
	}

	if b[0] < 32 || b[0] >= 127 {
		return tls.Server(newConn, l.Config), nil
	}

	return newConn, nil
}
