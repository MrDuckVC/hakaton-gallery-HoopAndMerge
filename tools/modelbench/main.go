package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	worldfiles "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/world"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
)

type fixture struct {
	profile domain.Profile
	data    guide.ContextData
}

func (f *fixture) Profile(string) (domain.Profile, error) { return f.profile.Clone(), nil }
func (*fixture) Exhibit(string) (world.Exhibit, error)    { return world.Exhibit{}, world.NotFound }
func (f *fixture) ReadContext(context.Context, string, string) (guide.ContextData, error) {
	return f.data, nil
}

type settings struct {
	model, base, phase, answers string
	repeats                     int
	cold                        bool
}

func main() {
	var s settings
	flag.StringVar(&s.model, "model", "", "exact prepared local tag (required)")
	flag.StringVar(&s.base, "url", "http://127.0.0.1:11434", "local Ollama URL")
	flag.StringVar(&s.phase, "phase", "baseline", "measurement label; never inferred scene/offline status")
	flag.IntVar(&s.repeats, "repeats", 3, "suite repetitions")
	flag.BoolVar(&s.cold, "cold", false, "unload model before every request (load only, not OS disk cache)")
	flag.StringVar(&s.answers, "answers", "", "optional private synthetic-answer JSONL file; never commit")
	flag.Parse()
	if s.model == "" || s.repeats < 1 {
		fmt.Fprintln(os.Stderr, "model and positive repeats required")
		os.Exit(2)
	}
	if err := run(context.Background(), s, os.Stdout, startTelemetry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, s settings, output io.Writer, sampler func(context.Context) func(*result)) error {
	client := newClient(10 * time.Second)
	var answers *os.File
	if s.answers != "" {
		var err error
		answers, err = os.OpenFile(s.answers, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer func() { _ = answers.Close() }()
	}
	for repeat := range s.repeats {
		for _, name := range []string{"leonardo", "vangogh"} {
			body, err := os.ReadFile("examples/guides/" + name + ".json")
			if err != nil {
				return err
			}
			p, err := worldfiles.ParseProfile(body)
			if err != nil {
				return err
			}
			f := &fixture{profile: p}
			builder := guide.Context{Profiles: f, Reader: f}
			for i, q := range []string{"Я осмотрел картину. Мне интересен контраст света и тени. Мой личный код ЭТЮД-742. Объясни приём и предложи короткое упражнение, не более 150 слов.", "Какой мой личный код и что меня интересовало? Продолжи прежнее упражнение, до 100 слов.", "Какой личный код я называл? Если в моей истории его нет, прямо скажи об этом, до 60 слов."} {
				if i == 2 {
					f.data = guide.ContextData{}
				}
				// Build excludes the current generation ID from completed history.
				// Synthetic questions therefore need distinct identities too.
				g := domain.Generation{ID: fmt.Sprintf("benchmark-%d-%s-%d", repeat, name, i), UserID: "synthetic", GuideID: name, UserMessage: domain.Message{Text: q}}
				input, e := builder.Build(ctx, g)
				if e != nil {
					return e
				}
				payload := request{Model: s.model, Stream: true}
				for _, m := range input.Messages {
					payload.Messages = append(payload.Messages, message{m.Role, m.Content})
				}
				r, e := perform(ctx, client, s, payload, fmt.Sprintf("%s-%d", name, i), repeat, answers, output, sampler)
				if e != nil {
					return e
				}
				if r.Status == "completed" {
					g.Status = "completed"
					g.AssistantMessage = &domain.Message{Text: r.Answer}
					f.data.Completed = append(f.data.Completed, g)
					f.data.Summary = guide.Summarize(f.data.Completed)
				}
			}
		}
		// Synthetic worst-case input at the application's character limit, with
		// a canary at the start to expose context truncation. No private data.
		prefix := "Ты учебный гид. Ответь по-русски кратко. Контрольный код: СПЕКТР-915.\n"
		suffix := "\nНазови контрольный код из начала сообщения и объясни светотень одним предложением."
		padding := strings.Repeat("Описание экспоната: свет и тень помогают воспринимать форму. ", 400)
		padding = string([]rune(padding)[:16000-utf8.RuneCountInString(prefix+suffix)])
		_, err := perform(ctx, client, s, request{Model: s.model, Stream: true, Messages: []message{{"user", prefix + padding + suffix}}}, "context-16000", repeat, answers, output, sampler)
		if err != nil {
			return err
		}
	}
	return nil
}

func perform(ctx context.Context, client *http.Client, s settings, payload request, scenario string, repeat int, answers *os.File, output io.Writer, sampler func(context.Context) func(*result)) (result, error) {
	if s.cold {
		body, _ := json.Marshal(map[string]any{"model": s.model, "keep_alive": 0})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/api/generate", strings.NewReader(string(body)))
		if err != nil {
			return result{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return result{}, err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return result{}, errors.New("unload failed")
		}
	}
	stop := sampler(ctx)
	deadline, cancel := context.WithTimeout(ctx, 90*time.Second)
	r := measure(deadline, client, s.base, payload, nil)
	cancel()
	stop(&r)
	r.ColdRunner = s.cold
	r.Model = s.model
	r.Phase = s.phase
	r.Scenario = scenario
	r.Repeat = repeat
	if err := json.NewEncoder(output).Encode(r); err != nil {
		return r, err
	}
	if answers != nil {
		if err := json.NewEncoder(answers).Encode(map[string]any{"scenario": scenario, "repeat": repeat, "status": r.Status, "answer": r.Answer}); err != nil {
			return r, err
		}
	}
	return r, nil
}

// newClient matches production dial/header/body deadlines. The header duration
// is explicit so the timeout path can be tested without a ten-second sleep.
func newClient(headerTimeout time.Duration) *http.Client {
	return &http.Client{Timeout: 90 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{Proxy: nil,
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ResponseHeaderTimeout: headerTimeout, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 1, MaxIdleConnsPerHost: 1}}
}
