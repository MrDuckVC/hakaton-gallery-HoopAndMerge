package guide

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

type contextFixture struct {
	data        ContextData
	user, guide string
}

func (f *contextFixture) ReadContext(_ context.Context, u, g string) (ContextData, error) {
	f.user = u
	f.guide = g
	return f.data, nil
}

type profileFixture struct{}

func (profileFixture) Profile(string) (domain.Profile, error) {
	return domain.Profile{Personality: "Леонардо", Style: "наблюдение", Topic: "живопись", Constraints: []string{"Не придумывать цитаты"}, Materials: []domain.Material{{ID: "b", Text: "other", Sources: []string{"catalog"}}, {ID: "a", Text: "Свет картины", Sources: []string{"catalog"}, ExhibitIDs: []string{"ex"}}}}, nil
}
func TestContextPrivateCompletedAndBudget(t *testing.T) {
	reader := &contextFixture{data: ContextData{Summary: "сохранённая память", Events: []Action{{Type: "EXHIBIT_INSPECTED", ExhibitID: "ex", At: time.Now()}, {Type: "PHOTO_CREATED", PhotoID: "photo"}}, Completed: []domain.Generation{{Status: "failed", UserMessage: domain.Message{Text: "failed secret"}}, {Status: "completed", UserMessage: domain.Message{Text: "прошлый вопрос"}, AssistantMessage: &domain.Message{Text: "прошлый ответ"}}}}}
	c := Context{Profiles: profileFixture{}, Reader: reader}
	req, err := c.Build(context.Background(), domain.Generation{ID: "current", UserID: "u", GuideID: "g", ExhibitID: "ex", UserMessage: domain.Message{Text: "Новый вопрос"}})
	if err != nil {
		t.Fatal(err)
	}
	var joined strings.Builder
	size := 0
	for _, m := range req.Messages {
		joined.WriteString(m.Content)
		size += utf8.RuneCountInString(m.Content)
	}
	if reader.user != "u" || reader.guide != "g" || !strings.Contains(joined.String(), "прошлый ответ") || !strings.Contains(joined.String(), "PHOTO_CREATED") || strings.Contains(joined.String(), "failed secret") || size > 16000 {
		t.Fatal(req)
	}
	reader.data.Summary = strings.Repeat("я", 8000)
	for range 10 {
		reader.data.Completed = append(reader.data.Completed, domain.Generation{Status: "completed", UserMessage: domain.Message{Text: strings.Repeat("界", 2000)}, AssistantMessage: &domain.Message{Text: strings.Repeat("界", 8000)}})
	}
	req, err = c.Build(context.Background(), domain.Generation{ID: "current", UserID: "u", GuideID: "g", UserMessage: domain.Message{Text: strings.Repeat("в", 2000)}})
	if err != nil {
		t.Fatal(err)
	}
	size = 0
	for _, m := range req.Messages {
		size += utf8.RuneCountInString(m.Content)
	}
	if size > 16000 || req.Messages[len(req.Messages)-1].Content != strings.Repeat("в", 2000) {
		t.Fatal(size)
	}
}
func TestDeterministicMemoryBoundedCompleted(t *testing.T) {
	items := make([]domain.Generation, 0, 6)
	for range 6 {
		items = append(items, domain.Generation{Status: "completed", UserMessage: domain.Message{Text: strings.Repeat("в", 2000)}, AssistantMessage: &domain.Message{Text: strings.Repeat("о", 8000)}})
	}
	a := Summarize(items)
	if utf8.RuneCountInString(a) > 4000 || a == "" || a != Summarize(items) {
		t.Fatal("invalid bounded summary")
	}
	if Summarize([]domain.Generation{{Status: "failed", AssistantMessage: &domain.Message{Text: "partial"}}}) != "" {
		t.Fatal("partial memory")
	}
}

func (profileFixture) Exhibit(id string) (world.Exhibit, error) {
	return world.Exhibit{ID: id, Title: "Картина", Description: "Светотень"}, nil
}

type failingContext struct {
	err     error
	entered chan struct{}
}

func (f failingContext) ReadContext(ctx context.Context, _, _ string) (ContextData, error) {
	if f.entered != nil {
		close(f.entered)
		<-ctx.Done()
		return ContextData{}, ctx.Err()
	}
	return ContextData{}, f.err
}
func TestContextFailureAndCancellationNeverCallsModel(t *testing.T) {
	for _, mode := range []string{"failure", "cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			service, store, events, in := fixture()
			ctx := t.Context()
			service.Ask(ctx, in, "ask", "guide", "question", "", func() Failure { return Failure{} })
			g, err := store.Claim(ctx, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			reader := failingContext{err: errors.New("database unavailable")}
			timer := make(chan time.Time, 1)
			if mode != "failure" {
				reader.entered = make(chan struct{})
			}
			service.ConfigureContext(profileFixture{}, reader)
			service.clock.After = func(time.Duration) <-chan time.Time { return timer }
			service.model = modelFunc(func(context.Context, ModelRequest, func(string) error) error {
				t.Error("model called without saved context")
				return nil
			})
			done := make(chan struct{})
			go func() { defer close(done); service.execute(ctx, g) }()
			if reader.entered != nil {
				<-reader.entered
				if mode == "cancel" {
					service.Cancel(ctx, in, "cancel", g.ID)
				} else {
					timer <- time.Now()
				}
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("context build did not stop")
			}
			result := store.records[g.ID]
			want := "AI_UNAVAILABLE"
			if mode == "cancel" {
				want = "CANCELLED"
			}
			if mode == "timeout" {
				want = "AI_TIMEOUT"
			}
			if result.Error == nil || result.Error.Code != want || store.memory != 0 || events.closed != 0 {
				t.Fatal(result, store.memory, events.closed)
			}
		})
	}
}

type rankedProfiles struct {
	profileFixture
	profile domain.Profile
}

func (p rankedProfiles) Profile(string) (domain.Profile, error) { return p.profile.Clone(), nil }
func TestMaterialRankingAndOldestHistoryTruncation(t *testing.T) {
	p, _ := profileFixture{}.Profile("g")
	p.Materials = []domain.Material{
		{ID: "z", Text: "selected-z", Sources: []string{"catalog"}, ExhibitIDs: []string{"selected"}},
		{ID: "a", Text: "selected-a", Sources: []string{"catalog"}, ExhibitIDs: []string{"selected"}},
		{ID: "c", Text: "inspected-c", Sources: []string{"catalog"}, ExhibitIDs: []string{"seen"}},
		{ID: "b", Text: "unrelated-b", Sources: []string{"catalog"}},
	}
	reader := &contextFixture{data: ContextData{Events: []Action{{ExhibitID: "seen", At: time.Now()}}}}
	builder := Context{Profiles: rankedProfiles{profile: p}, Reader: reader}
	g := domain.Generation{ID: "current", UserID: "u", GuideID: "g", ExhibitID: "selected", UserMessage: domain.Message{Text: "вопрос"}}
	req, err := builder.Build(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	content := req.Messages[1].Content
	if (strings.Index(content, "selected-a") >= strings.Index(content, "selected-z") || strings.Index(content, "selected-z") >= strings.Index(content, "inspected-c")) || strings.Contains(content, "unrelated-b") {
		t.Fatal("material order changed")
	}
	p.Materials = nil
	builder.Profiles = rankedProfiles{profile: p}
	for _, id := range []string{"oldest", "older", "old", "newest"} {
		reader.data.Completed = append(reader.data.Completed, domain.Generation{ID: id, Status: "completed", UserMessage: domain.Message{Text: id + strings.Repeat("я", 1900)}, AssistantMessage: &domain.Message{Text: strings.Repeat("ю", 7900)}})
	}
	req, err = builder.Build(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 5 || !strings.HasPrefix(req.Messages[2].Content, "newest") {
		t.Fatal("must retain only latest full pair under cap", len(req.Messages))
	}
}

func TestContextUsesClaimedProfileSnapshot(t *testing.T) {
	p, _ := profileFixture{}.Profile("g")
	p.Personality = "captured original"
	g := domain.Generation{UserID: "u", GuideID: "g", UserMessage: domain.Message{Text: "question"}, ProfileSnapshot: &domain.Snapshot{Profile: p.Clone(), Revision: 2}}
	c := Context{Profiles: profileFixture{}, Reader: &contextFixture{}}
	req, err := c.Build(t.Context(), g)
	if err != nil || !strings.Contains(req.Messages[0].Content, "captured original") {
		t.Fatal(req, err)
	}
	if !strings.Contains(req.Messages[0].Content, instructions) {
		t.Fatal("fixed system rules lost")
	}
}
