package websocket

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ws "github.com/coder/websocket"
)

// Model coder/websocket: CloseNow can join a graceful Close already in progress.
// Only aborting the underlying network connection guarantees it will unblock.
type closingHeartbeatSocket struct {
	started       chan struct{}
	releasePing   chan struct{}
	networkClosed chan struct{}
	once          sync.Once
}

func (*closingHeartbeatSocket) Write(context.Context, ws.MessageType, []byte) error { return nil }
func (c *closingHeartbeatSocket) Ping(context.Context) error {
	close(c.started)
	<-c.releasePing
	return errors.New("missing pong")
}
func (c *closingHeartbeatSocket) CloseNow() error { <-c.networkClosed; return nil }
func (c *closingHeartbeatSocket) abort()          { c.once.Do(func() { close(c.networkClosed) }) }

func TestHeartbeatTimeoutAbortsAnOverlappingCloseHandshake(t *testing.T) {
	socket := &closingHeartbeatSocket{started: make(chan struct{}), releasePing: make(chan struct{}), networkClosed: make(chan struct{})}
	p := newPeer(socket)
	p.abort = socket.abort
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); p.heartbeat(ctx, time.Millisecond, time.Second) }()
	defer func() { socket.abort(); <-done }()
	<-socket.started
	close(socket.releasePing)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat waited for close handshake instead of aborting the network")
	}
	if !p.isClosing() {
		t.Fatal("timed-out peer still admits events")
	}
}
