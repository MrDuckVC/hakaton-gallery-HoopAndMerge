package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMeasureRequiresTerminalAndKeepsMissingMetricsNull(t *testing.T) {
	for _, tc := range []struct{ name, body, status string }{
		{"complete", "{\"message\":{\"content\":\"Привет\"}}\n{\"done\":true,\"eval_count\":4,\"eval_duration\":2000000000}\n", "completed"},
		{"truncated", "{\"message\":{\"content\":\"Привет\"}}\n", "incomplete_stream"},
		{"error", "{\"error\":\"out of memory\"}\n", "ollama_error"},
		{"empty", "{\"done\":true}\n", "empty_response"},
		{"invalid", "not-json\n", "invalid_stream"},
		{"wrong-role", "{\"message\":{\"role\":\"user\",\"content\":\"text\"},\"done\":true}\n", "invalid_stream"},
		{"nul", "{\"message\":{\"content\":\"a\\u0000b\"},\"done\":true}\n", "invalid_stream"},
		{"surrogate", "{\"message\":{\"content\":\"\\ud800\"},\"done\":true}\n", "invalid_stream"},
		{"whitespace", "{\"message\":{\"content\":\"   \"},\"done\":true}\n", "empty_response"},
		{"length", "{\"done\":true,\"done_reason\":\"length\"}\n", "output_limit"},
		{"oversize", fmt.Sprintf("{\"message\":{\"content\":%q}}\n{\"done\":true}\n", strings.Repeat("Я", 8001)), "output_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			r := measure(context.Background(), srv.Client(), srv.URL, request{}, nil)
			if r.Status != tc.status {
				t.Fatalf("status=%s want %s", r.Status, tc.status)
			}
			if r.LoadSeconds != nil {
				t.Fatal("missing load metric must remain null")
			}
			if tc.name == "error" && !r.OOM {
				t.Fatal("OOM was not recorded")
			}
			if tc.name == "complete" && (r.Characters != 6 || r.TokensPerSecond == nil || *r.TokensPerSecond != 2 || r.TTFTSeconds == nil) {
				t.Fatalf("incorrect metrics: %+v", r)
			}
		})
	}
}

func TestClientRejectsDelayedHeadersLikeProduction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := measure(ctx, newClient(20*time.Millisecond), srv.URL, request{}, nil)
	if r.Status != "timeout" || r.TTFTSeconds != nil || ctx.Err() != nil {
		t.Fatalf("delayed headers accepted: %+v", r)
	}
}

func TestMeasureThinkingDoesNotCountAsVisibleTokenAndHonorsDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "{\"message\":{\"thinking\":\"reasoning\"}}\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	r := measure(ctx, srv.Client(), srv.URL, request{}, nil)
	if r.Status != "timeout" || r.TTFTSeconds != nil {
		t.Fatalf("incorrect result: %+v", r)
	}
}

func TestParseGPUUnavailableAndPhysicalMemory(t *testing.T) {
	if parseGPU("[N/A], [N/A], [N/A]") != nil {
		t.Fatal("unavailable GPU must be null")
	}
	g := parseGPU("8188, 2200, 5748")
	if g == nil || g.UsedMiB != 2200 || g.FreeMiB != 5748 {
		t.Fatalf("GPU: %+v", g)
	}
	m := parseMemory("MemTotal:       16000 kB\nMemAvailable:    6000 kB\n")
	if m == nil || m.UsedKiB != 10000 {
		t.Fatalf("memory: %+v", m)
	}
}

func TestPeaksSurviveMissingAndLowerSamples(t *testing.T) {
	var r result
	r.observe(&gpuMemory{8188, 4000, 3948}, &hostMemory{16000, 10000, 6000}, &hostMemory{32000, 22000, 10000})
	r.observe(nil, nil, nil)
	r.observe(&gpuMemory{8188, 2000, 5948}, &hostMemory{16000, 11000, 5000}, &hostMemory{32000, 20000, 12000})
	if r.Samples != 3 || r.PeakGPU.UsedMiB != 4000 || r.PeakWSL.UsedKiB != 11000 || r.PeakWindows.UsedKiB != 22000 {
		t.Fatalf("lost peak: %+v", r)
	}
}

func TestColdUnloadFailureDoesNotEmitMeasurement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Error("generation started before successful unload")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	var out strings.Builder
	_, err := perform(context.Background(), srv.Client(), settings{cold: true, model: "prepared:tag", base: srv.URL}, request{}, "cold", 0, nil, &out, func(context.Context) func(*result) { t.Fatal("sample started before unload"); return nil })
	if err == nil || out.Len() != 0 {
		t.Fatal("failed unload produced benchmark result")
	}
}
