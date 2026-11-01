package game

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Metrics is a browser-reported sample, not a trusted performance benchmark.
type Metrics struct {
	FPS     *float64
	Visible bool
}

// ClientMetric contains only the current administrative view of a connected player.
type ClientMetric struct {
	UserID, DisplayName, RoomID string
	FPS, PingMS                 *float64
	Visible                     bool
	SampledAt                   *time.Time
	Stale                       bool
}

type telemetry struct {
	fps, pingMS            *float64
	visible                bool
	sampledAt              time.Time
	nextProbe, probeSentAt time.Time
	nonce                  string
	sequence               uint64
	lastPongSequence       uint64
	metrics, pongs         []time.Time
}

func telemetryType(kind string) bool { return kind == "client.metrics" || kind == "telemetry.pong" }

// telemetryReceipt is server-owned evidence captured before session I/O.
// It cannot be supplied by a wire payload or reused for a replacement connection.
type telemetryReceipt struct {
	userID, connectionID string
	at, probeSentAt      time.Time
	sequence             uint64
}

// ReceiveTelemetry records arrival and the challenge outstanding at that instant.
// Authentication must still succeed before Handle commits any measurements.
func (s *Service) ReceiveTelemetry(userID, id string, cmd Command) Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.active[userID]
	if c != nil && c.id == id && !c.ended && !s.stopping {
		cmd.telemetryReceipt = telemetryArrival(c, userID, id, cmd.Nonce, s.now())
	}
	return cmd
}

func telemetryArrival(c *connection, userID, id, nonce string, now time.Time) *telemetryReceipt {
	r := &telemetryReceipt{userID: userID, connectionID: id, at: now}
	t := &c.telemetry
	if t.nonce != "" && nonce == t.nonce && now.Sub(t.probeSentAt) < 5*time.Second {
		r.probeSentAt, r.sequence = t.probeSentAt, t.sequence
	}
	return r
}

func copyNumber(value *float64) *float64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

// Clients snapshots live connections without persistence or network I/O.
func (s *Service) Clients() []ClientMetric {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	clients := make([]ClientMetric, 0, len(s.active))
	for _, c := range s.active {
		if c.ended {
			continue
		}
		t := &c.telemetry
		metric := ClientMetric{UserID: c.identity.User.ID, DisplayName: c.identity.User.DisplayName, RoomID: c.identity.State.RoomID, Visible: t.visible, Stale: t.sampledAt.IsZero() || now.Sub(t.sampledAt) >= 6*time.Second}
		if !t.sampledAt.IsZero() {
			sample := t.sampledAt
			metric.SampledAt = &sample
		}
		if !metric.Stale {
			metric.FPS = copyNumber(t.fps)
		}
		if t.nonce == "" || now.Sub(t.probeSentAt) < 5*time.Second {
			metric.PingMS = copyNumber(t.pingMS)
		}
		clients = append(clients, metric)
	}
	sort.Slice(clients, func(i, j int) bool { return clients[i].UserID < clients[j].UserID })
	return clients
}

func (s *Service) handleTelemetry(userID, id string, cmd Command) Failure {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.active[userID]
	if c == nil || c.id != id || c.ended || s.stopping {
		return Failure{Drop: true}
	}
	t := &c.telemetry
	receipt := cmd.telemetryReceipt
	if receipt == nil {
		receipt = telemetryArrival(c, userID, id, cmd.Nonce, s.now())
	}
	if receipt.userID != userID || receipt.connectionID != id {
		return Failure{Drop: true}
	}
	if cmd.Type == "client.metrics" {
		fps := cmd.Metrics.FPS
		if fps != nil && (!cmd.Metrics.Visible || math.IsNaN(*fps) || math.IsInf(*fps, 0) || *fps < 0 || *fps > 1000) {
			return Failure{Code: "VALIDATION_ERROR"}
		}
		t.fps, t.visible, t.sampledAt = copyNumber(fps), cmd.Metrics.Visible, receipt.at
		return Failure{}
	}
	if receipt.sequence > t.lastPongSequence {
		rtt := float64(receipt.at.Sub(receipt.probeSentAt)) / float64(time.Millisecond)
		t.pingMS = &rtt
		t.lastPongSequence = receipt.sequence
		if cmd.Nonce == t.nonce {
			t.nonce = ""
		}
	}
	return Failure{}
}

// PublishTelemetry queues per-connection challenges after initial world snapshots.
// The regular game clock drives this method; no separate network writer is created.
func (s *Service) PublishTelemetry() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	now := s.now()
	for _, c := range s.active {
		if c.ended {
			continue
		}
		t := &c.telemetry
		if t.nonce != "" && now.Sub(t.probeSentAt) >= 5*time.Second {
			t.nonce, t.pingMS = "", nil
		}
		if now.Before(t.nextProbe) {
			continue
		}
		t.sequence++
		nonce := fmt.Sprintf("%s_%016x", c.id, t.sequence)
		if c.sink.Enqueue(Event{Type: "telemetry.probe", Nonce: nonce}) {
			t.nonce, t.probeSentAt = nonce, now
		}
		t.nextProbe = now.Add(5 * time.Second)
	}
}
