package proxy

import (
	"io"
	"net"
	"sync/atomic"
)

// countedReader wraps an io.ReadCloser and reports each Read's byte count to
// an external atomic counter. Used to instrument request body uploads so the
// live "connections" view can show bytes-in growing during transfer.
type countedReader struct {
	r io.ReadCloser
	n *atomic.Int64
}

func newCountedReader(r io.ReadCloser, n *atomic.Int64) *countedReader {
	return &countedReader{r: r, n: n}
}

func (c *countedReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.n != nil {
		c.n.Add(int64(n))
	}
	return n, err
}

func (c *countedReader) Close() error { return c.r.Close() }

// countedConn wraps a net.Conn so the proxy can count bytes through a
// hijacked TCP tunnel (CONNECT) in both directions. `in` is bytes the
// client sent toward the upstream/target; `out` is bytes coming back.
type countedConn struct {
	net.Conn
	in  *atomic.Int64
	out *atomic.Int64
}

// wrapClientConn instruments a client-side connection: data the client
// writes is bytes-in for the proxied flow; data we write back is bytes-out.
func wrapClientConn(c net.Conn, in, out *atomic.Int64) *countedConn {
	return &countedConn{Conn: c, in: in, out: out}
}

func (c *countedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 && c.in != nil {
		c.in.Add(int64(n))
	}
	return n, err
}

func (c *countedConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 && c.out != nil {
		c.out.Add(int64(n))
	}
	return n, err
}
