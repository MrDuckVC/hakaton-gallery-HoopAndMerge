package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	auth "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	guide "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/guide"
)

func TestStreamSplitUTF8AndFinalChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/chat" {
			t.Error("wrong endpoint")
		}
		var req struct {
			Model    string              `json:"model"`
			Stream   bool                `json:"stream"`
			Messages []map[string]string `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != "prepared:tag" || !req.Stream || len(req.Messages) != 1 || req.Messages[0]["content"] != "вопрос" {
			t.Error("wrong request")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, b := range []byte("{\"message\":{\"role\":\"assistant\",\"content\":\"Привет\"},\"done\":false}\n{\"message\":{\"content\":\"!\"},\"done\":true,\"done_reason\":\"stop\"}\n") {
			if _, err := w.Write([]byte{b}); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	got := ""
	err := New(server.URL, "prepared:tag").Stream(t.Context(), guide.ModelRequest{Messages: []guide.ModelMessage{{Role: "user", Content: "вопрос"}}}, func(s string) error { got += s; return nil })
	if err != nil || got != "Привет!" {
		t.Fatal(got, err)
	}
}
func TestStreamFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       error
	}{
		{"http", "private server failure", 503, auth.Unavailable},
		{"error", `{"error":"private prompt"}`, 200, auth.Unavailable},
		{"broken", `{"message":`, 200, auth.Unavailable},
		{"eof", `{"message":{"content":"partial"},"done":false}`, 200, auth.Unavailable},
		{"long", strings.Repeat("x", 256*1024+1), 200, auth.Unavailable},
		{"utf8", string([]byte{'{', '"', 'x', '"', ':', '"', 255, '"', '}'}), 200, auth.Unavailable},
		{"nul", `{"message":{"content":"\u0000"},"done":true}`, 200, auth.Unavailable},
		{"limit", `{"done":true,"done_reason":"length"}`, 200, auth.Code("AI_OUTPUT_LIMIT")},
		{"lone high surrogate", `{"message":{"content":"\ud800"},"done":true}`, 200, auth.Unavailable},
		{"lone low surrogate", `{"message":{"content":"\udfff"},"done":true}`, 200, auth.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			err := New(srv.URL, "tag").Stream(t.Context(), guide.ModelRequest{}, func(string) error { return nil })
			if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
		})
	}
}
func TestStreamCancelTimeoutAndCallback(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "callback"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{})
			ended := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(ended)
				_, _ = w.Write([]byte("{\"message\":{\"content\":\"partial\"},\"done\":false}\n"))
				w.(http.Flusher).Flush()
				close(entered)
				<-r.Context().Done()
			}))
			defer srv.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "timeout" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 50*time.Millisecond)
				defer stop()
			}
			sentinel := errors.New("stop callback")
			done := make(chan error, 1)
			go func() {
				done <- New(srv.URL, "tag").Stream(ctx, guide.ModelRequest{}, func(string) error {
					if mode == "callback" {
						return sentinel
					}
					return nil
				})
			}()
			<-entered
			if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-done:
				if mode == "callback" && !errors.Is(err, sentinel) {
					t.Fatal(err)
				}
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("stream stuck")
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("request not closed")
			}
		})
	}
}

func TestHTTPClientTimeoutIsDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer srv.Close()
	client := New(srv.URL, "tag")
	client.http.Timeout = 20 * time.Millisecond
	err := client.Stream(t.Context(), guide.ModelRequest{}, func(string) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout must be classified as deadline: %v", err)
	}
}

func TestValidUnicodeEscapesAndLiteralBackslashes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"content":"\ud83c\udfa8 \\ud800"},"done":true}`))
	}))
	defer server.Close()
	text := ""
	if err := New(server.URL, "tag").Stream(t.Context(), guide.ModelRequest{}, func(s string) error { text += s; return nil }); err != nil || text != "🎨 \\ud800" {
		t.Fatal(text, err)
	}
}
