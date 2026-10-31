package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestFallbackContract(t *testing.T) {
	server := NewServer("127.0.0.1:0")
	var previous string
	for range 2 {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil)
		request.Header.Set("X-Request-ID", "untrusted-client-id")
		server.Handler.ServeHTTP(response, request)
		var body struct {
			Error struct {
				Code, Message string
				RequestID     string `json:"request_id"`
			}
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		id := response.Header().Get("X-Request-ID")
		if response.Code != 404 || body.Error.Code != "NOT_FOUND" || body.Error.Message == "" || id == "" || id == previous || id == "untrusted-client-id" || id != body.Error.RequestID || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatalf("invalid response: %d %v %s", response.Code, response.Header(), response.Body)
		}
		previous = id
	}
}

func TestStopAcceptingAndDrainHTTP(t *testing.T) {
	server := NewServer("127.0.0.1:0")
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+listener.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if err := server.StopAccepting(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("listener did not stop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	responseRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(responseRecorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if responseRecorder.Code != 503 {
		t.Fatalf("accepted new work during shutdown: %d", responseRecorder.Code)
	}
}

// pausedAcceptListener keeps Serve registered until Shutdown has closed its listener.
// This reproduces the StopAccepting/Shutdown ordering without relying on scheduling.
type pausedAcceptListener struct {
	net.Listener
	accepting chan struct{}
	release   chan struct{}
	closeErr  error
}

func (l *pausedAcceptListener) Accept() (net.Conn, error) {
	close(l.accepting)
	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.release
	}
	return conn, err
}

func (l *pausedAcceptListener) Close() error {
	if err := l.Listener.Close(); err != nil {
		return err
	}
	return l.closeErr
}

func TestShutdownImmediatelyAfterStopAccepting(t *testing.T) {
	closeFailure := errors.New("listener close failed")
	for _, tc := range []struct {
		name     string
		closeErr error
	}{{"graceful", nil}, {"preserve_close_error", closeFailure}} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			listener := &pausedAcceptListener{Listener: raw, accepting: make(chan struct{}), release: make(chan struct{}), closeErr: tc.closeErr}
			release := sync.OnceFunc(func() { close(listener.release) })
			server := NewServer(raw.Addr().String())
			server.RegisterOnShutdown(release)
			t.Cleanup(func() { release(); _ = server.Close(); _ = raw.Close() })
			done := make(chan error, 1)
			go func() { done <- server.Serve(listener) }()
			select {
			case <-listener.accepting:
			case <-time.After(time.Second):
				t.Fatal("Serve did not start accepting")
			}
			err = server.StopAccepting(context.Background())
			if !errors.Is(err, tc.closeErr) {
				t.Fatalf("StopAccepting: want %v, got %v", tc.closeErr, err)
			}
			// Do not wait for Serve: its delayed Accept guarantees it is still registered.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = server.Shutdown(ctx)
			if !errors.Is(err, tc.closeErr) {
				t.Fatalf("Shutdown: want %v, got %v", tc.closeErr, err)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("Serve did not finish")
			}
		})
	}
}
