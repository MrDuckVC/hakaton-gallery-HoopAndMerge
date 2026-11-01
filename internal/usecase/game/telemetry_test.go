package game

import (
	"math"
	"strconv"
	"sync"
	"testing"
	"time"
)

func telemetryCommand(fps *float64, visible bool) Command {
	return Command{Type: "client.metrics", Metrics: Metrics{FPS: fps, Visible: visible}}
}

func TestTelemetrySamplesAndStaleness(t *testing.T) {
	now := time.Now()
	s := New(func() time.Time { return now })
	id := attach(t, s, "a", &testSink{})
	initial := s.Clients()[0]
	if initial.FPS != nil || initial.PingMS != nil || initial.SampledAt != nil || initial.Visible || !initial.Stale {
		t.Fatal(initial)
	}
	fps := 59.5
	if f := s.Handle(t.Context(), "a", id, telemetryCommand(&fps, true)); f.Code != "" || f.Drop {
		t.Fatal(f)
	}
	fps = 999 // Caller-owned pointers must not change the stored sample.
	got := s.Clients()[0]
	if got.FPS == nil || *got.FPS != 59.5 || !got.Visible || got.Stale || !got.SampledAt.Equal(now) {
		t.Fatal(got)
	}
	*got.FPS = 123 // Returned snapshots must not expose mutable internal pointers.
	now = now.Add(6*time.Second - time.Nanosecond)
	if got = s.Clients()[0]; got.Stale || *got.FPS != 59.5 {
		t.Fatal(got)
	}
	now = now.Add(time.Nanosecond)
	if got = s.Clients()[0]; !got.Stale || got.FPS != nil || got.SampledAt == nil {
		t.Fatal(got)
	}
	s.Handle(t.Context(), "a", id, telemetryCommand(nil, false))
	if got = s.Clients()[0]; got.Stale || got.Visible || got.FPS != nil || !got.SampledAt.Equal(now) {
		t.Fatal(got)
	}
	s.Handle(t.Context(), "a", id, telemetryCommand(nil, true))
	if got = s.Clients()[0]; got.Stale || !got.Visible || got.FPS != nil {
		t.Fatal(got)
	}
	for _, bad := range []float64{-1, 1001, math.NaN(), math.Inf(1)} {
		if f := s.Handle(t.Context(), "a", id, telemetryCommand(&bad, true)); f.Code != "VALIDATION_ERROR" {
			t.Fatal(bad, f)
		}
	}
	if got = s.Clients()[0]; !got.Visible || got.FPS != nil || !got.SampledAt.Equal(now) {
		t.Fatal("invalid samples changed state", got)
	}
	if f := s.Handle(t.Context(), "a", id, telemetryCommand(&fps, false)); f.Code != "VALIDATION_ERROR" {
		t.Fatal(f)
	}
}

func TestTelemetryTwoSecondSamplesAndRuntimeOrdering(t *testing.T) {
	now := time.Now()
	s := New(func() time.Time { return now })
	s.ConfigureRuntime(runtimeFixture())
	sink := &testSink{}
	id := attach(t, s, "a", sink)
	for i := range 4 {
		fps := float64(60 + i)
		cmd := telemetryCommand(&fps, true)
		if f := s.Check("a", id, cmd); f.Code != "" || f.Drop {
			t.Fatal(f)
		}
		s.Handle(t.Context(), "a", id, cmd)
		if got := s.Clients()[0]; got.Stale || !got.SampledAt.Equal(now) || *got.FPS != fps {
			t.Fatal(got)
		}
		now = now.Add(2 * time.Second)
		s.PublishTelemetry()
	}
	if len(sink.events) != 3 || sink.events[0].Type != "world.snapshot" || sink.events[1].Type != "world.runtime" || sink.events[2].Type != "telemetry.probe" {
		t.Fatal(sink.events)
	}
	// At t=8 the sample at t=6 is fresh; at t=12 it is stale.
	now = now.Add(4 * time.Second)
	if got := s.Clients()[0]; !got.Stale || got.FPS != nil {
		t.Fatal(got)
	}
}

func TestTelemetryProbeNonceAndTimeout(t *testing.T) {
	now := time.Now()
	s := New(func() time.Time { return now })
	sink := &testSink{}
	id := attach(t, s, "a", sink)
	now = now.Add(5*time.Second - time.Nanosecond)
	s.PublishTelemetry()
	if len(sink.events) != 1 {
		t.Fatal(sink.events)
	}
	now = now.Add(time.Nanosecond)
	s.PublishTelemetry()
	probe := sink.events[len(sink.events)-1]
	if probe.Type != "telemetry.probe" || len(probe.Nonce) < 16 || probe.RequestID != "" {
		t.Fatal(probe)
	}
	now = now.Add(125 * time.Millisecond)
	s.Handle(t.Context(), "a", id, Command{Type: "telemetry.pong", Nonce: "unknown_nonce_1234"})
	if s.Clients()[0].PingMS != nil {
		t.Fatal("unknown nonce accepted")
	}
	s.Handle(t.Context(), "a", id, Command{Type: "telemetry.pong", Nonce: probe.Nonce})
	if got := s.Clients()[0].PingMS; got == nil || *got != 125 {
		t.Fatal(got)
	}
	now = now.Add(time.Second)
	s.Handle(t.Context(), "a", id, Command{Type: "telemetry.pong", Nonce: probe.Nonce})
	if *s.Clients()[0].PingMS != 125 {
		t.Fatal("duplicate changed RTT")
	}
	now = now.Add(3875 * time.Millisecond)
	s.PublishTelemetry()
	second := sink.events[len(sink.events)-1]
	if second.Nonce == probe.Nonce {
		t.Fatal("nonce reused")
	}
	now = now.Add(5*time.Second - time.Nanosecond)
	if s.Clients()[0].PingMS == nil {
		t.Fatal("premature timeout")
	}
	now = now.Add(time.Nanosecond)
	if s.Clients()[0].PingMS != nil {
		t.Fatal("timeout did not clear ping")
	}
	s.Handle(t.Context(), "a", id, Command{Type: "telemetry.pong", Nonce: second.Nonce})
	if s.Clients()[0].PingMS != nil {
		t.Fatal("expired nonce accepted")
	}
	s.PublishTelemetry()
	third := sink.events[len(sink.events)-1]
	s.Handle(t.Context(), "a", id, Command{Type: "telemetry.pong", Nonce: second.Nonce})
	s.Handle(t.Context(), "a", id, Command{Type: "telemetry.pong", Nonce: third.Nonce})
	if got := s.Clients()[0].PingMS; got == nil || *got != 0 {
		t.Fatal(got)
	}
}

func TestTelemetryArrivalSurvivesAuthenticationDelayAndProbeRotation(t *testing.T) {
	now := time.Now()
	s := New(func() time.Time { return now })
	sink := &testSink{}
	id := attach(t, s, "a", sink)
	now = now.Add(5 * time.Second)
	s.PublishTelemetry()
	oldProbe := sink.events[len(sink.events)-1]
	now = now.Add(12 * time.Millisecond)
	cmd := s.ReceiveTelemetry("a", id, Command{Type: "telemetry.pong", Nonce: oldProbe.Nonce})
	// Authentication is external I/O; during it the next challenge can be queued.
	now = now.Add(5 * time.Second)
	s.PublishTelemetry()
	newProbe := sink.events[len(sink.events)-1]
	s.Handle(t.Context(), "a", id, cmd)
	if got := s.Clients()[0].PingMS; got == nil || *got != 12 {
		t.Fatal("authentication latency included or timely pong lost", got)
	}
	// Completion of the old pong must not consume the new outstanding challenge.
	now = now.Add(time.Millisecond)
	newCmd := s.ReceiveTelemetry("a", id, Command{Type: "telemetry.pong", Nonce: newProbe.Nonce})
	s.Handle(t.Context(), "a", id, newCmd)
	s.Handle(t.Context(), "a", id, cmd)
	if got := s.Clients()[0].PingMS; got == nil || *got != 1 {
		t.Fatal("older completion overwrote newer RTT", got)
	}
	current := attach(t, s, "a", &testSink{})
	s.Handle(t.Context(), "a", current, cmd)
	if s.Clients()[0].PingMS != nil {
		t.Fatal("receipt reused on replacement")
	}
}

func TestTelemetryIndependentAdmission(t *testing.T) {
	now := time.Now()
	s := New(func() time.Time { return now })
	id := attach(t, s, "a", &testSink{})
	for i := range 30 {
		s.Check("a", id, Command{Type: "world.ready", RequestID: strconv.Itoa(i)})
	}
	for _, kind := range []string{"client.metrics", "telemetry.pong"} {
		for i := range 3 {
			f := s.Check("a", id, Command{Type: kind})
			if f.Code != "" || f.Drop != (i == 2) {
				t.Fatal(kind, i, f)
			}
		}
	}
	if len(s.active["a"].seen) != 30 {
		t.Fatal("telemetry entered UUID budget")
	}
	for range 10001 {
		s.Check("a", id, Command{Type: "client.metrics"})
	}
	if !s.Current("a", id) || len(s.active["a"].seen) != 30 {
		t.Fatal("telemetry exhausted UUID lifetime")
	}
	now = now.Add(time.Second)
	for _, kind := range []string{"client.metrics", "telemetry.pong"} {
		if f := s.Check("a", id, Command{Type: kind}); f.Code != "" || f.Drop {
			t.Fatal(f)
		}
	}
	if f := s.Check("a", id, Command{Type: "world.ready", RequestID: "extra"}); f.Code != "RATE_LIMITED" {
		t.Fatal(f)
	}
	now = now.Add(time.Second)
	if f := s.Check("a", id, Command{Type: "client.metrics"}); f.Code != "" || f.Drop {
		t.Fatal(f)
	}
}

func TestTelemetrySnapshotCapacityRoomAndConcurrentReplacement(t *testing.T) {
	s := New(time.Now)
	ids := make([]string, 32)
	for i := range 32 {
		ids[i] = attach(t, s, strconv.Itoa(i), &testSink{})
	}
	s.mu.Lock()
	s.active["0"].identity.State.RoomID = "new-room"
	s.mu.Unlock()
	if clients := s.Clients(); len(clients) != 32 || clients[0].RoomID != "new-room" {
		t.Fatal(clients)
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 100 {
			fps := 60.0
			s.Handle(t.Context(), "0", ids[0], telemetryCommand(&fps, true))
			s.Check("0", ids[0], Command{Type: "telemetry.pong"})
		}
	})
	workers.Go(func() {
		for range 100 {
			s.Clients()
			s.PublishTelemetry()
		}
	})
	workers.Go(func() {
		current := attach(t, s, "0", &testSink{})
		s.Detach("0", ids[0])
		s.End("0", current, 4001)
	})
	workers.Wait()
	if len(s.Clients()) != 31 {
		t.Fatal("ended connection exposed")
	}
}

func TestTelemetryReplacementDisconnectAndIsolation(t *testing.T) {
	now := time.Now()
	s := New(func() time.Time { return now })
	a, b := &testSink{}, &testSink{}
	old := attach(t, s, "a", a)
	attach(t, s, "b", b)
	fps := 60.0
	s.Handle(t.Context(), "a", old, telemetryCommand(&fps, true))
	now = now.Add(5 * time.Second)
	s.PublishTelemetry()
	oldNonce := a.events[len(a.events)-1].Nonce
	currentSink := &testSink{}
	current := attach(t, s, "a", currentSink)
	s.Handle(t.Context(), "a", old, telemetryCommand(&fps, true))
	s.Handle(t.Context(), "a", current, Command{Type: "telemetry.pong", Nonce: oldNonce})
	s.Detach("a", old)
	for _, metric := range s.Clients() {
		if metric.FPS != nil || metric.PingMS != nil || metric.SampledAt != nil {
			t.Fatal(metric)
		}
	}
	// Detach marks ended before it can acquire the persistence gate.
	release, err := s.acquire(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.Detach("a", current); close(done) }()
	deadline := time.Now().Add(time.Second)
	for len(s.Clients()) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := s.Clients(); len(got) != 1 || got[0].UserID != "b" {
		t.Error(got)
	}
	release()
	<-done
	for _, event := range b.events {
		if event.Type == "client.metrics" || event.Type == "telemetry.pong" || (event.Type == "telemetry.probe" && event.Nonce == oldNonce) {
			t.Fatal("telemetry broadcast", event)
		}
	}
}
