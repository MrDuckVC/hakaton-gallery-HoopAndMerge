// Package main provides an opt-in hardware benchmark, never a normal GPU test.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type result struct {
	ColdRunner      bool        `json:"cold_runner"`
	StartedAt       string      `json:"started_at"`
	OOM             bool        `json:"oom_reported"`
	Model           string      `json:"model"`
	Phase           string      `json:"phase"`
	Scenario        string      `json:"scenario"`
	Repeat          int         `json:"repeat"`
	Status          string      `json:"status"`
	Seconds         float64     `json:"seconds"`
	TTFTSeconds     *float64    `json:"ttft_seconds"`
	LoadSeconds     *float64    `json:"load_seconds"`
	TokensPerSecond *float64    `json:"tokens_per_second"`
	PromptTokens    *int        `json:"prompt_tokens"`
	OutputTokens    *int        `json:"output_tokens"`
	Characters      int         `json:"characters"`
	PeakGPU         *gpuMemory  `json:"peak_gpu"`
	PeakWSL         *hostMemory `json:"peak_wsl"`
	PeakWindows     *hostMemory `json:"peak_windows"`
	Samples         int         `json:"samples"`
	Answer          string      `json:"-"`
}

// measure deliberately uses the same request shape as the production adapter:
// all model options come from a prepared Modelfile, not benchmark-only overrides.
func measure(ctx context.Context, client *http.Client, base string, payload request, sample func(*result)) result {
	r := result{Status: "incomplete_stream"}
	started := time.Now()
	r.StartedAt = started.UTC().Format(time.RFC3339Nano)
	body, err := json.Marshal(payload)
	if err != nil {
		r.Status = "request_error"
		return r
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/chat", bytes.NewReader(body))
	if err != nil {
		r.Status = "request_error"
		return r
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		r.Status = failure(ctx, err)
		r.Seconds = time.Since(started).Seconds()
		return r
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		r.OOM = containsOOM(string(body))
		r.Status = "http_error"
		r.Seconds = time.Since(started).Seconds()
		return r
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 256*1024+2)
	for scanner.Scan() {
		var v struct {
			Message         message `json:"message"`
			Done            bool    `json:"done"`
			DoneReason      string  `json:"done_reason"`
			Error           string  `json:"error"`
			LoadDuration    *int64  `json:"load_duration"`
			EvalCount       *int    `json:"eval_count"`
			EvalDuration    *int64  `json:"eval_duration"`
			PromptEvalCount *int    `json:"prompt_eval_count"`
		}
		if !utf8.Valid(scanner.Bytes()) || !unicodeEscapes(scanner.Bytes()) || json.Unmarshal(scanner.Bytes(), &v) != nil {
			r.Status = "invalid_stream"
			break
		}
		if strings.ContainsRune(v.Message.Content, 0) || (v.Message.Role != "" && v.Message.Role != "assistant") {
			r.Status = "invalid_stream"
			break
		}
		if v.Error != "" {
			r.Status = "ollama_error"
			r.OOM = containsOOM(v.Error)
			break
		}
		if v.DoneReason == "length" {
			r.Status = "output_limit"
			break
		}
		if v.Message.Content != "" {
			if r.TTFTSeconds == nil {
				n := time.Since(started).Seconds()
				r.TTFTSeconds = &n
			}
			r.Characters += utf8.RuneCountInString(v.Message.Content)
			if r.Characters > 8000 {
				r.Status = "output_limit"
				break
			}
			r.Answer += v.Message.Content
		}
		if v.Done {
			r.Status = "completed"
			if strings.TrimSpace(r.Answer) == "" {
				r.Status = "empty_response"
			}
			r.PromptTokens = v.PromptEvalCount
			r.OutputTokens = v.EvalCount
			if v.LoadDuration != nil {
				n := float64(*v.LoadDuration) / 1e9
				r.LoadSeconds = &n
			}
			if v.EvalCount != nil && v.EvalDuration != nil && *v.EvalDuration > 0 {
				n := float64(*v.EvalCount) / (float64(*v.EvalDuration) / 1e9)
				r.TokensPerSecond = &n
			}
			break
		}
	}
	if scanner.Err() != nil {
		r.Status = failure(ctx, scanner.Err())
	}
	r.Seconds = time.Since(started).Seconds()
	if sample != nil {
		sample(&r)
	}
	return r
}

func containsOOM(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "out of memory") || strings.Contains(s, "cuda_error_out_of_memory")
}

func failure(ctx context.Context, err error) string {
	var timeout net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return "timeout"
	}
	if ctx.Err() != nil {
		return "cancelled"
	}
	return "transport_error"
}
