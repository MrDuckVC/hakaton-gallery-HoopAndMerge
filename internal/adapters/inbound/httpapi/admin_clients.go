package httpapi

import (
	"net/http"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
)

// Clients reads only current, in-memory client measurements.
type Clients interface {
	Clients() []game.ClientMetric
}

// ConfigureClients connects telemetry before HTTP admission starts.
func (s *Server) ConfigureClients(source Clients) { s.clients = source }

type clientMetricDTO struct {
	UserID      string     `json:"user_id"`
	DisplayName string     `json:"display_name"`
	RoomID      string     `json:"room_id"`
	FPS         *float64   `json:"fps"`
	PingMS      *float64   `json:"ping_ms"`
	Visible     bool       `json:"visible"`
	SampledAt   *time.Time `json:"sampled_at"`
	Stale       bool       `json:"stale"`
}

func (s *Server) adminClients(w http.ResponseWriter) {
	if s.clients == nil {
		writeError(w, 503, "SERVICE_UNAVAILABLE", "Телеметрия недоступна", 0)
		return
	}
	metrics := s.clients.Clients()
	clients := make([]clientMetricDTO, len(metrics))
	for i, metric := range metrics {
		clients[i] = clientMetricDTO{UserID: metric.UserID, DisplayName: metric.DisplayName, RoomID: metric.RoomID, FPS: metric.FPS, PingMS: metric.PingMS, Visible: metric.Visible, SampledAt: metric.SampledAt, Stale: metric.Stale}
	}
	writeJSON(w, 200, struct {
		Clients []clientMetricDTO `json:"clients"`
	}{clients})
}
