package guide

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	auth "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/guide"
)

// UnavailableModel explicitly represents B07 production until B08 supplies Ollama.
type UnavailableModel struct{}

// Stream never manufactures an answer when a real model is not configured.
func (UnavailableModel) Stream(context.Context, domain.Generation, func(string) error) error {
	return auth.Unavailable
}

// Run owns one model worker and independent queue expiry. Cancellation joins both.
func (s *Service) Run(ctx context.Context) {
	var workers sync.WaitGroup
	workers.Go(func() { s.work(ctx) })
	defer workers.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.clock.After(100 * time.Millisecond):
			s.sweep(ctx)
			s.notify()
		}
	}
}
func (s *Service) sweep(ctx context.Context) {
	if err := s.acquire(ctx); err != nil {
		return
	}
	defer s.release()
	if s.stopping {
		return
	}
	if s.dirty.Load() {
		if s.recoverLocked(ctx) != nil {
			return
		}
	}
	pending, err := s.store.Pending(ctx)
	if err != nil {
		return
	}
	for _, g := range pending {
		if g.Status == "queued" && !s.clock.Now().Before(g.CreatedAt.Add(120*time.Second)) {
			if _, _, err = s.finishLocked(ctx, g, "AI_TIMEOUT", ""); err != nil {
				return
			}
		}
	}
}
func (s *Service) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
		for {
			if err := s.acquire(ctx); err != nil {
				return
			}
			if s.stopping || s.dirty.Load() {
				s.release()
				break
			}
			g, err := s.store.Claim(ctx, s.clock.Now())
			s.release()
			if err != nil {
				if !missing(err) {
					s.dirty.Store(true)
				}
				break
			}
			s.execute(ctx, g)
		}
	}
}
func (s *Service) execute(ctx context.Context, g domain.Generation) {
	defer s.notify()
	modelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := s.acquire(ctx); err != nil {
		s.dirty.Store(true)
		s.events.CloseGuideConnection(g.UserID, g.ConnectionID, 1011)
		return
	}
	current, err := s.store.Get(ctx, g.UserID, g.ID)
	if err != nil {
		s.dirty.Store(true)
		s.events.CloseGuideConnection(g.UserID, g.ConnectionID, 1011)
	}
	if err != nil || current.Terminal() || s.stopping || s.dirty.Load() {
		s.release()
		return
	}
	s.runningID = g.ID
	s.cancelModel = cancel
	s.events.Emit(g.UserID, g.ConnectionID, Event{Type: "guide.started", RequestID: g.RequestID, GenerationID: g.ID})
	s.release()
	text := ""
	index := 0
	code := ""
	done := make(chan error, 1)
	go func() {
		done <- s.model.Stream(modelCtx, g, func(delta string) error {
			if acquireErr := s.acquire(modelCtx); acquireErr != nil {
				return acquireErr
			}
			defer s.release()
			latest, readErr := s.store.Get(modelCtx, g.UserID, g.ID)
			if readErr != nil && modelCtx.Err() == nil {
				s.dirty.Store(true)
				s.events.CloseGuideConnection(g.UserID, g.ConnectionID, 1011)
			}
			if readErr != nil || latest.Terminal() {
				return context.Canceled
			}
			if !utf8.ValidString(delta) || strings.ContainsRune(delta, 0) {
				code = "AI_UNAVAILABLE"
				return auth.Unavailable
			}
			if utf8.RuneCountInString(text)+utf8.RuneCountInString(delta) > 8000 {
				code = "AI_OUTPUT_LIMIT"
				return auth.Unavailable
			}
			text += delta
			if !s.events.Emit(g.UserID, g.ConnectionID, Event{Type: "guide.delta", RequestID: g.RequestID, GenerationID: g.ID, Index: index, Text: delta}) {
				code = "CANCELLED"
				return context.Canceled
			}
			index++
			return nil
		})
	}()
	timeout := s.clock.After(max(0, g.StartedAt.Add(90*time.Second).Sub(s.clock.Now())))
	select {
	case err = <-done:
	case <-timeout:
		cancel()
		err = <-done
		code = "AI_TIMEOUT"
	case <-modelCtx.Done():
		cancel()
		err = <-done
	}
	// Never overlap a model that has not acknowledged cancellation with another job.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if s.acquire(finishCtx) != nil {
		s.dirty.Store(true)
		s.events.CloseGuideConnection(g.UserID, g.ConnectionID, 1011)
		return
	}
	defer s.release()
	s.runningID = ""
	s.cancelModel = nil
	if s.dirty.Load() {
		return
	}
	if err != nil && code == "" {
		code = "AI_UNAVAILABLE"
		if errors.Is(ctx.Err(), context.Canceled) {
			code = "CANCELLED"
		}
	}
	_, _, _ = s.finishLocked(finishCtx, g, code, text)
}
