package telemetry

import (
	"io"
	"net"
	"sync"
	"testing"
)

// blackholeProxy forwards TCP until outage. Connections open at the outage
// stay open but silent, like a NAT reset behind a dropping firewall; new
// connections after restore are forwarded again.
type blackholeProxy struct {
	listener net.Listener
	target   string
	mu       sync.Mutex
	epoch    int
	down     bool
}

func newBlackholeProxy(t *testing.T, target string) *blackholeProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &blackholeProxy{listener: listener, target: target}
	t.Cleanup(func() { _ = listener.Close() })
	go p.serve()
	return p
}

func (p *blackholeProxy) Addr() string { return p.listener.Addr().String() }

func (p *blackholeProxy) Outage() {
	p.mu.Lock()
	p.epoch++
	p.down = true
	p.mu.Unlock()
}

func (p *blackholeProxy) Restore() {
	p.mu.Lock()
	p.down = false
	p.mu.Unlock()
}

func (p *blackholeProxy) alive(epoch int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.epoch == epoch && !p.down
}

func (p *blackholeProxy) serve() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		epoch, down := p.epoch, p.down
		p.mu.Unlock()
		if down {
			go func() { _, _ = io.Copy(io.Discard, conn) }()
			continue
		}
		upstream, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = conn.Close()
			continue
		}
		go p.pipe(upstream, conn, epoch)
		go p.pipe(conn, upstream, epoch)
	}
}

func (p *blackholeProxy) pipe(dst, src net.Conn, epoch int) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 && p.alive(epoch) {
			_, _ = dst.Write(buf[:n])
		}
		if err != nil {
			_ = dst.Close()
			return
		}
	}
}
