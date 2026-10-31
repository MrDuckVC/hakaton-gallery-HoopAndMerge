package game

import (
	"context"

	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
)

// GuideCommands delegates durable idempotency before ready and new-question limits.
type GuideCommands interface {
	Ask(context.Context, guide.Input, string, string, string, string, guide.Admission) guide.Failure
	Cancel(context.Context, guide.Input, string, string) guide.Failure
}

// ConfigureGuides installs question commands before admission starts.
func (s *Service) ConfigureGuides(commands GuideCommands) { s.guides = commands }

func (s *Service) guideCommand(ctx context.Context, in guide.Input, cmd Command) Failure {
	if s.guides == nil {
		if cmd.Type == "guide.ask" && !in.Ready {
			return Failure{Code: "GAME_NOT_READY"}
		}
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	var f guide.Failure
	if cmd.Type == "guide.cancel" {
		f = s.guides.Cancel(ctx, in, cmd.RequestID, cmd.GenerationID)
	} else {
		f = s.guides.Ask(ctx, in, cmd.RequestID, cmd.GuideID, cmd.Text, cmd.ExhibitID, func() guide.Failure {
			result := s.AdmitNewAsk(in.Session.UserID, in.ConnectionID)
			if result.Drop {
				return guide.Failure{Code: "SERVICE_UNAVAILABLE"}
			}
			return guide.Failure{Code: result.Code, RetryAfterMS: result.RetryAfterMS}
		})
	}
	return Failure{Code: f.Code, RetryAfterMS: f.RetryAfterMS}
}

// Emit never acquires the user operation gate: guide callbacks can run inside it.
func (s *Service) Emit(user, id string, e guide.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.active[user]
	if c == nil || c.id != id || c.ended {
		return false
	}
	return c.sink.Enqueue(Event{Type: e.Type, RequestID: e.RequestID, Guide: e})
}

// CloseGuideConnection is nonblocking and connection-scoped, including during cancellation.
func (s *Service) CloseGuideConnection(user, id string, code int) { s.closeCurrent(user, id, code) }
