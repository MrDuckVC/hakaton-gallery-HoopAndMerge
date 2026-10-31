package httpapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
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
