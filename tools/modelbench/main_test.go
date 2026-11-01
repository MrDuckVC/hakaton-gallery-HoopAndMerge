package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRunFixturesIsolatesHistoryAndDoesNotLogAnswers(t *testing.T) {
	// Verify the full completed pair even when Summary still retains the canary;
	// fresh personalities/repeats and the third request must have no old pair.
	root, rootErr := filepath.Abs("../..")
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	t.Chdir(root)
	calls := 0
	var previousQuestion, previousAnswer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Model    string
			Messages []message
			Stream   bool
			Options  any
			Think    any
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Error(err)
			return
		}
		var text strings.Builder
		for _, m := range p.Messages {
			text.WriteString(m.Content)
		}
		if p.Model != "prepared:tag" || !p.Stream || p.Options != nil || p.Think != nil {
			t.Error("benchmark overrides production request shape")
		}
		scenario := calls % 7
		if scenario < 6 {
			wantCode := scenario%3 != 2
			if strings.Contains(text.String(), "ЭТЮД-742") != wantCode {
				t.Errorf("history isolation failed at request %d", calls)
			}
			if scenario%3 == 1 {
				if len(p.Messages) != 5 {
					t.Errorf("follow-up %d: want 5 messages including completed pair, got %d", calls, len(p.Messages))
				} else if p.Messages[2].Role != "user" || p.Messages[2].Content != previousQuestion ||
					p.Messages[3].Role != "assistant" || p.Messages[3].Content != previousAnswer {
					t.Errorf("follow-up %d: previous question/full answer missing or changed", calls)
				}
			} else {
				if len(p.Messages) != 3 {
					t.Errorf("fresh history %d: want 3 messages, got %d", calls, len(p.Messages))
				}
				for _, m := range p.Messages {
					if m.Role == "assistant" {
						t.Errorf("fresh history %d contains a completed answer", calls)
					}
				}
			}
		} else if utf8.RuneCountInString(text.String()) != 16000 || !strings.Contains(text.String(), "СПЕКТР-915") {
			t.Error("incorrect long-context fixture")
		}
		if scenario == 0 || scenario == 3 {
			previousQuestion = p.Messages[len(p.Messages)-1].Content
		}
		// The tail is beyond Summary's 700-point answer excerpt. Only a full
		// completed assistant message can preserve the entire previous answer.
		previousAnswer = fmt.Sprintf("Ответ %d ЭТЮД-742 %s конец полного ответа", calls, strings.Repeat("я", 720))
		calls++
		if encodeErr := json.NewEncoder(w).Encode(map[string]any{"message": message{Role: "assistant", Content: previousAnswer}, "done": true}); encodeErr != nil {
			t.Error(encodeErr)
		}
	}))
	defer srv.Close()
	var out bytes.Buffer
	answerPath := filepath.Join(t.TempDir(), "answers.jsonl")
	s := settings{model: "prepared:tag", base: srv.URL, phase: "test", repeats: 2, answers: answerPath}
	noHardware := func(context.Context) func(*result) { return func(*result) {} }
	if err := run(context.Background(), s, &out, noHardware); err != nil {
		t.Fatal(err)
	}
	if calls != 14 || bytes.Contains(out.Bytes(), []byte("ЭТЮД-742")) {
		t.Fatalf("calls=%d or full answer leaked to stdout", calls)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		var r result
		if json.Unmarshal(line, &r) != nil || r.Status != "completed" || r.PeakGPU != nil {
			t.Fatalf("incorrect result: %s", line)
		}
	}
	b, err := os.ReadFile(answerPath)
	if err != nil || !bytes.Contains(b, []byte("ЭТЮД-742")) {
		t.Fatal("private answers missing", err)
	}
	// Existing files must survive a second run intact.
	if runErr := run(context.Background(), s, &out, noHardware); runErr == nil {
		t.Fatal("existing answer file was overwritten")
	}
	after, err := os.ReadFile(answerPath)
	if err != nil || !bytes.Equal(b, after) {
		t.Fatal("existing answers changed", err)
	}
}
