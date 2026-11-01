package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/worldwire"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/ollama"
	worldfiles "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/world"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/world"
)

type localHistory struct{ data guide.ContextData }

func (h *localHistory) ReadContext(ctx context.Context, _, _ string) (guide.ContextData, error) {
	return h.data, ctx.Err()
}

// TestLocalModelGallery uses real C01 profiles and production context/transport.
// Optional private synthetic answers support manual fact/style review; stdout
// contains only counts/timings. No model is downloaded or selected implicitly.
func TestLocalModelGallery(t *testing.T) {
	model := os.Getenv("C01_OLLAMA_MODEL")
	if model == "" {
		t.Skip("C01_OLLAMA_MODEL not set; prepared local Ollama required")
	}
	base := os.Getenv("C01_OLLAMA_URL")
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	catalog, err := world.Load(t.Context(), worldfiles.Files{Dir: "../../content/world"}, worldwire.SnapshotSizer{}, "gallery_c01_v1")
	if err != nil {
		t.Fatal(err)
	}
	var answers *os.File
	if path := os.Getenv("C01_ANSWERS_FILE"); path != "" {
		answers, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := answers.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	for _, selection := range []struct{ guideID, exhibitID, comparison string }{{"guide_leonardo", "exhibit_nga_50724", "Сопоставь №50724, №21 и №12131; до 120 слов."}, {"guide_monet", "exhibit_nga_61379", "Сопоставь №61379, №52204 и №46681; до 120 слов."}, {"guide_vangogh", "exhibit_nga_106382", "Сопоставь №106382, №46625 и №66418; до 120 слов."}} {
		history := &localHistory{}
		builder := guide.Context{Profiles: catalog, Reader: history}
		for i, q := range []string{"Представься и обозначь учебную роль. Назови автора, название и музейную датировку выбранной картины. Объясни один приём в своём стиле и предложи короткое упражнение, до 120 слов.", selection.comparison} {
			generation := domain.Generation{ID: fmt.Sprintf("c01-%s-%d", selection.guideID, i), UserID: "synthetic-c01", GuideID: selection.guideID, ExhibitID: selection.exhibitID, UserMessage: domain.Message{Text: q}}
			input, err := builder.Build(t.Context(), generation)
			if err != nil {
				t.Fatal(err)
			}
			if i > 0 {
				previous := history.data.Completed[len(history.data.Completed)-1].AssistantMessage.Text
				found := false
				for _, message := range input.Messages {
					if message.Role == "assistant" && message.Content == previous {
						found = true
					}
				}
				if !found {
					t.Fatal("full previous completed answer was evicted from the context")
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			var answer strings.Builder
			started := time.Now()
			err = ollama.New(base, model).Stream(ctx, input, func(s string) error {
				answer.WriteString(s)
				if utf8.RuneCountInString(answer.String()) > 8000 {
					return errors.New("output exceeds 8000 code points")
				}
				return nil
			})
			cancel()
			if err != nil {
				t.Fatalf("%s question %d: %v", selection.guideID, i+1, err)
			}
			if strings.TrimSpace(answer.String()) == "" {
				t.Fatal("empty answer")
			}
			elapsed := time.Since(started)
			t.Logf("guide=%s question=%d code_points=%d elapsed=%s", selection.guideID, i+1, utf8.RuneCountInString(answer.String()), elapsed)
			if answers != nil {
				if err := json.NewEncoder(answers).Encode(map[string]any{"guide_id": selection.guideID, "exhibit_id": selection.exhibitID, "question": i + 1, "answer": answer.String(), "elapsed_ms": elapsed.Milliseconds()}); err != nil {
					t.Fatal(err)
				}
			}
			generation.Status = "completed"
			generation.AssistantMessage = &domain.Message{Text: answer.String()}
			history.data.Completed = append(history.data.Completed, generation)
			history.data.Summary = guide.Summarize(history.data.Completed)
		}
	}
}
