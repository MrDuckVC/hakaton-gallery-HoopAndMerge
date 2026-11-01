package game

import (
	"context"
	"testing"

	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
)

type questionCommands struct {
	ready bool
	calls int
}

func (q *questionCommands) Ask(_ context.Context, in guide.Input, _, _, _, _ string, _ guide.Admission) guide.Failure {
	q.ready = in.Ready
	q.calls++
	return guide.Failure{}
}
func (*questionCommands) Cancel(context.Context, guide.Input, string, string) guide.Failure {
	return guide.Failure{}
}
func TestGuideAskDelegatesBeforeReady(t *testing.T) {
	s, _, id, _, _ := setupGame(t)
	q := &questionCommands{}
	s.ConfigureGuides(q)
	if f := s.Handle(context.Background(), "a", id, Command{Type: "guide.ask", RequestID: "ask", GuideID: "guide", Text: "q"}); f.Code != "" || q.calls != 1 || q.ready {
		t.Fatal(f, q)
	}
}
