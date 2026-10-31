package ollama

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	worldfiles "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/world"
	auth "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/guide"
	world "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
	guide "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/guide"
)

type demoContext struct {
	profile domain.Profile
	data    guide.ContextData
}

func (d *demoContext) Profile(string) (domain.Profile, error) { return d.profile.Clone(), nil }
func (d *demoContext) ReadContext(context.Context, string, string) (guide.ContextData, error) {
	return d.data, nil
}

// TestLocalModelFixtures is opt-in and never downloads or chooses a model tag.
// Ordinary tests do not require Ollama or a GPU. Only counts/timing are logged.
func TestLocalModelFixtures(t *testing.T) {
	model := os.Getenv("B08_OLLAMA_MODEL")
	if model == "" {
		t.Skip("B08_OLLAMA_MODEL not set; real-model check is opt-in")
	}
	base := os.Getenv("B08_OLLAMA_URL")
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	for _, name := range []string{"leonardo", "vangogh"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile("../../../../examples/guides/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			p, err := worldfiles.ParseProfile(body)
			if err != nil {
				t.Fatal(err)
			}
			demo := &demoContext{profile: p}
			builder := guide.Context{Profiles: demo, Reader: demo}
			for i, q := range []string{"Я осмотрел картину: объясни, как свет и цвет помогают её понять.", "Продолжи предыдущее объяснение и предложи упражнение."} {
				g := domain.Generation{ID: name + string(rune('a'+i)), UserID: "demo", GuideID: name, UserMessage: domain.Message{Text: q}}
				input, e := builder.Build(t.Context(), g)
				if e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
				var answer strings.Builder
				started := time.Now()
				e = New(base, model).Stream(ctx, input, func(s string) error {
					if utf8.RuneCountInString(answer.String())+utf8.RuneCountInString(s) > 8000 {
						return auth.Code("AI_OUTPUT_LIMIT")
					}
					answer.WriteString(s)
					return nil
				})
				cancel()
				if e != nil {
					t.Fatal(e)
				}
				if strings.TrimSpace(answer.String()) == "" {
					t.Fatal("empty model response")
				}
				g.Status = "completed"
				g.AssistantMessage = &domain.Message{Text: answer.String()}
				demo.data.Completed = append(demo.data.Completed, g)
				demo.data.Summary = guide.Summarize(demo.data.Completed)
				t.Logf("question=%d response_code_points=%d elapsed=%s", i+1, utf8.RuneCountInString(answer.String()), time.Since(started))
			}
		})
	}
}

func (*demoContext) Exhibit(string) (world.Exhibit, error) { return world.Exhibit{}, world.NotFound }
