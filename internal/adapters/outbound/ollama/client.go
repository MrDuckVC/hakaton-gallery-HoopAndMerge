// Package ollama implements the local HTTP/NDJSON model boundary without an SDK.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
)

// Client sends only usecase-assembled messages to the configured model server.
type Client struct {
	baseURL, model string
	http           *http.Client
}

// New uses an isolated transport without environment proxies or redirects.
// Configuration is validated at the composition boundary; construction never contacts Ollama.
func New(baseURL, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"), model: model,
		http: &http.Client{
			Timeout:       90 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ResponseHeaderTimeout: 10 * time.Second,
				IdleConnTimeout:       90 * time.Second,
				MaxIdleConns:          1,
				MaxIdleConnsPerHost:   1,
			},
		},
	}

}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type request struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Stream   bool      `json:"stream"`
}
type chunk struct {
	Message    message `json:"message"`
	Done       bool    `json:"done"`
	DoneReason string  `json:"done_reason"`
	Error      string  `json:"error"`
}

func streamFailure(ctx context.Context, stage string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("ollama %s: %w", stage, auth.Unavailable)
}

// Stream emits complete UTF-8 text additions synchronously; success requires done=true.
func (c *Client) Stream(ctx context.Context, input guide.ModelRequest, emit func(string) error) error {
	payload := request{Model: c.model, Stream: true, Messages: make([]message, 0, len(input.Messages))}
	for _, m := range input.Messages {
		payload.Messages = append(payload.Messages, message{Role: m.Role, Content: m.Content})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return streamFailure(ctx, "encode")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return streamFailure(ctx, "request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
	response, err := c.http.Do(req)
	if err != nil {
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return context.DeadlineExceeded
		}
		return streamFailure(ctx, "request")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return streamFailure(ctx, "status")
	}
	const maxRecord = 256 * 1024
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), maxRecord+2)
	for scanner.Scan() {
		if err = ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) > maxRecord || !utf8.Valid(line) || !unicodeEscapes(line) {
			return streamFailure(ctx, "record")
		}
		var value chunk
		if err = json.Unmarshal(line, &value); err != nil || value.Error != "" || !utf8.ValidString(value.Message.Content) || strings.ContainsRune(value.Message.Content, 0) || (value.Message.Role != "" && value.Message.Role != "assistant") {
			return streamFailure(ctx, "decode")
		}
		if value.DoneReason == "length" {
			return auth.Code("AI_OUTPUT_LIMIT")
		}
		if value.Message.Content != "" {
			if err = emit(value.Message.Content); err != nil {
				return err
			}
		}
		if value.Done {
			return nil
		}
	}
	var timeout net.Error
	if errors.As(scanner.Err(), &timeout) && timeout.Timeout() {
		return context.DeadlineExceeded
	}
	return streamFailure(ctx, "incomplete stream")
}

var _ guide.Model = (*Client)(nil)
