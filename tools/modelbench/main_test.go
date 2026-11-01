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
	// Broken history reset leaks the canary into each personality's third request;
	// broken context assembly loses it from the follow-up request.
	root, rootErr := filepath.Abs("../..")
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	t.Chdir(root)
	calls := 0
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
		if calls < 6 {
			wantCode := calls%3 != 2
			if strings.Contains(text.String(), "ЭТЮД-742") != wantCode {
				t.Errorf("history isolation failed at request %d", calls)
			}
		} else if utf8.RuneCountInString(text.String()) != 16000 || !strings.Contains(text.String(), "СПЕКТР-915") {
			t.Error("incorrect long-context fixture")
		}
		calls++
		_, _ = fmt.Fprint(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"Ответ ЭТЮД-742\"},\"done\":true}\n")
	}))
	defer srv.Close()
	var out bytes.Buffer
	answerPath := filepath.Join(t.TempDir(), "answers.jsonl")
	s := settings{model: "prepared:tag", base: srv.URL, phase: "test", repeats: 1, answers: answerPath}
	noHardware := func(context.Context) func(*result) { return func(*result) {} }
	if err := run(context.Background(), s, &out, noHardware); err != nil {
		t.Fatal(err)
	}
	if calls != 7 || bytes.Contains(out.Bytes(), []byte("ЭТЮД-742")) {
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
