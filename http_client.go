package httpretry

import (
	"net"
	"net/http"
	"time"
)

type dialer struct {
	Timeout    time.Duration
	KeepAlive  time.Duration
	Inactivity time.Duration
}

func (d *dialer) Dial(netw, addr string) (net.Conn, error) {
	c, err := net.DialTimeout(netw, addr, d.Timeout)
	if err != nil {
		return nil, err
	}
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(d.KeepAlive)
	}
	return &deadlineConn{d.Inactivity, c}, nil
}

// ClientWithTimeout is an http client optimized for high throughput.  It times
// out more agressively than the default http client in net/http as well as
// setting deadlines on the TCP connection.
//
// Taken from s3gof3r:
// https://github.com/rlmcpherson/s3gof3r/blob/1e759738ff170bd0381a848337db677dbdd6aa62/http_client.go
//
func ClientWithTimeout(timeout time.Duration) *http.Client {
	dialer := NewDialer(timeout)
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial:  dialer.Dial,
		ResponseHeaderTimeout: dialer.Inactivity,
		MaxIdleConnsPerHost:   10,
	}
	return &http.Client{Transport: transport}
}

// DialWithTimeout creates a Dial function that returns a connection with an
// inactivity timeout for the given duration.  This is designed for long running
// HTTP requests.
func DialWithTimeout(timeout time.Duration) func(netw, addr string) (net.Conn, error) {
	return NewDialer(timeout).Dial
}

// NewDialer creates a dialer object with configurable timeoutes for keep alive
// and inactivity.
func NewDialer(timeout time.Duration) *dialer {
	return &dialer{Timeout: timeout, KeepAlive: timeout, Inactivity: timeout}
}

type deadlineConn struct {
	Timeout time.Duration
	net.Conn
}

func (c *deadlineConn) Read(b []byte) (int, error) {
	if err := c.Conn.SetDeadline(time.Now().Add(c.Timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Read(b)
}

func (c *deadlineConn) Write(b []byte) (int, error) {
	if err := c.Conn.SetDeadline(time.Now().Add(c.Timeout)); err != nil {
		return 0, err
	}

	return c.Conn.Write(b)
}
