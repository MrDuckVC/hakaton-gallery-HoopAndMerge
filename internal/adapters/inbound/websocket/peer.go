package websocket

import (
	"context"
	"sync"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	ws "github.com/coder/websocket"
)

const maxEventBytes = 256 * 1024
const maxQueueBytes = 1024 * 1024

type socket interface {
	Write(context.Context, ws.MessageType, []byte) error
	Ping(context.Context) error
	CloseNow() error
}

type peer struct {
	conn      socket
	abort     func()
	mu        sync.Mutex
	queue     [][]byte
	bytes     int
	closing   bool
	wake      chan struct{}
	closeCode chan int
	done      chan struct{}
}

func newPeer(conn socket) *peer {
	return &peer{conn: conn, wake: make(chan struct{}, 1), closeCode: make(chan int, 1), done: make(chan struct{})}
}

// Enqueue serializes once and never waits for a socket or another goroutine.
func (p *peer) Enqueue(e game.Event) bool {
	b, err := encode(e)
	if err != nil || len(b) > maxEventBytes {
		p.Close(1011)
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		return false
	}
	if p.bytes+len(b) > maxQueueBytes {
		p.closeLocked(1013)
		return false
	}
	p.queue = append(p.queue, b)
	p.bytes += len(b)
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return true
}
func (p *peer) Close(code int) { p.mu.Lock(); defer p.mu.Unlock(); p.closeLocked(code) }
func (p *peer) closeLocked(code int) {
	if !p.closing {
		p.closing = true
		p.closeCode <- code
	}
}
func (p *peer) isClosing() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.closing }
func (p *peer) next() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing || len(p.queue) == 0 {
		return nil
	}
	b := p.queue[0]
	p.queue[0] = nil
	p.queue = p.queue[1:]
	return b
}
func (p *peer) written(n int) { p.mu.Lock(); defer p.mu.Unlock(); p.bytes -= n }

// write is the sole application-data writer; the library handles protocol control frames.
func (p *peer) write(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
			for {
				b := p.next()
				if b == nil {
					break
				}
				writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := p.conn.Write(writeCtx, ws.MessageText, b)
				cancel()
				p.written(len(b))
				if err != nil {
					p.Close(1013)
					return
				}
			}
		}
	}
}
func (p *peer) heartbeat(ctx context.Context, interval, timeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, timeout)
			err := p.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				p.Close(1008)
				// CloseNow can join a concurrent graceful Close in coder/websocket.
				// Abort the underlying socket first: a silent peer cannot finish
				// that handshake, and must not retain transport workers until it does.
				if p.abort != nil {
					p.abort()
				}
				_ = p.conn.CloseNow()
				return
			}
		}
	}
}
