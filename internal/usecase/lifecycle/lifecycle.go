// Package lifecycle defines shutdown orchestration and ports owned by its consumer.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
)

// Admission prevents new connections and commands. It must honor context cancellation.
type Admission interface{ StopAccepting(context.Context) error }

// Generations cancels in-flight generations and persists their terminal state.
type Generations interface{ CancelGenerations(context.Context) error }

// Positions flushes player positions before connections and storage close.
type Positions interface{ SavePositions(context.Context) error }

// Connections closes game connections with the server-restart close code (1012).
type Connections interface{ CloseConnections(context.Context) error }

// Coordinator orders shutdown; participants must return when their context expires.
type Coordinator struct {
	Admission   Admission
	Generations Generations
	Positions   Positions
	Connections Connections
}

// Shutdown attempts every registered cleanup step, including after failures or cancellation.
func (c Coordinator) Shutdown(ctx context.Context) error {
	var failures []error
	steps := []struct {
		name string
		run  func(context.Context) error
	}{
		{"stop admission", nil}, {"cancel generations", nil}, {"save positions", nil}, {"close connections", nil},
	}
	if c.Admission != nil {
		steps[0].run = c.Admission.StopAccepting
	}
	if c.Generations != nil {
		steps[1].run = c.Generations.CancelGenerations
	}
	if c.Positions != nil {
		steps[2].run = c.Positions.SavePositions
	}
	if c.Connections != nil {
		steps[3].run = c.Connections.CloseConnections
	}
	for _, step := range steps {
		if step.run != nil {
			if err := runStep(ctx, step.run); err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", step.name, err))
			}
		}
	}
	return errors.Join(failures...)
}

// runStep keeps a faulty participant from preventing subsequent cleanup attempts.
// After the deadline remaining steps receive the expired context and run best-effort.
func runStep(ctx context.Context, run func(context.Context) error) error {
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
