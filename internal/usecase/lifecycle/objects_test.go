package lifecycle

import (
	"context"
	"testing"
)

type objectCleanup struct{ called *bool }

func (o objectCleanup) ReleaseObjects(context.Context) error { *o.called = true; return nil }

type objectClose struct {
	called *bool
	t      *testing.T
}

func (o objectClose) CloseConnections(context.Context) error {
	if !*o.called {
		o.t.Error("connections closed before objects released")
	}
	return nil
}
func TestObjectsReleasedBeforeConnections(t *testing.T) {
	called := false
	c := Coordinator{Objects: objectCleanup{&called}, Connections: objectClose{&called, t}}
	if err := c.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}
