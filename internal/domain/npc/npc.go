// Package npc defines deterministic visitors independently of transport and storage.
package npc

import (
	"errors"
	"hash/fnv"
	"math"
	"strconv"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// State is a detached public visitor state in floor coordinates.
type State struct {
	ID              string
	Position        player.Vector
	Yaw             float64
	State           string
	SpeedMPS, Phase float64
}

// Visitor contains only the player information required by the FSM.
type Visitor struct {
	UserID   string
	Position player.Vector
}

type route struct{ from, to player.Vector }

// Plan contains immutable, prevalidated patrol segments for one room.
type Plan struct{ routes [runtime.MaxNPCCount]route }

// Prepare bounds geometry work to a 64x64 grid and ten endpoint searches.
// All checks happen before serving, never on a simulation tick.
func Prepare(room world.Room) (Plan, error) {
	var plan Plan
	points := make([]player.Vector, 0, 512)
	nx := min(64, int(math.Ceil((room.Max.X-room.Min.X)/.5)))
	nz := min(64, int(math.Ceil((room.Max.Z-room.Min.Z)/.5)))
	for ix := range nx {
		for iz := range nz {
			p := player.Vector{X: room.Min.X + (float64(ix)+.5)*(room.Max.X-room.Min.X)/float64(nx), Z: room.Min.Z + (float64(iz)+.5)*(room.Max.Z-room.Min.Z)/float64(nz)}
			if validSegment(room, p, p) {
				points = append(points, p)
			}
		}
	}
	if len(points) == 0 {
		return plan, errors.New("no safe NPC floor point")
	}
	for i := range plan.routes {
		h := fnv.New64a()
		// Lengths make concatenated opaque identifiers unambiguous.
		_, _ = h.Write([]byte(strconv.Itoa(len(room.Version)) + ":" + room.Version + strconv.Itoa(len(room.ID)) + ":" + room.ID + ":" + strconv.Itoa(i)))
		seed := h.Sum64()
		start := int(seed % uint64(len(points)))
		from := points[start]
		to := from
		offset := int((seed >> 32) % uint64(len(points)))
		for j := range len(points) {
			candidate := points[(offset+j)%len(points)]
			if distance(from, candidate) >= 1 && validSegment(room, from, candidate) {
				to = candidate
				break
			}
		}
		plan.routes[i] = route{from: from, to: to}
	}
	return plan, nil
}

func validSegment(room world.Room, from, to player.Vector) bool {
	if !world.Sweep(room, from, to) {
		return false
	}
	dx, dz := to.X-from.X, to.Z-from.Z
	length := dx*dx + dz*dz
	for _, d := range room.Doors {
		t := 0.0
		if length > 0 {
			t = max(0, min(1, ((d.Position.X-from.X)*dx+(d.Position.Z-from.Z)*dz)/length))
		}
		if math.Hypot(from.X+t*dx-d.Position.X, from.Z+t*dz-d.Position.Z) < d.Radius+.3 {
			return false
		}
	}
	return true
}

type actor struct {
	state           State
	route           route
	forward         bool
	speaker         Visitor
	spokenAt, until time.Time
	hasSpeaker      bool
}

// Simulation is owned and serialized by the game usecase.
type Simulation struct {
	plan   Plan
	actors []actor
}

// New creates visitors at deterministic initial positions.
func New(plan Plan, count int) *Simulation {
	s := &Simulation{plan: plan}
	s.Resize(count)
	return s
}

// Resize retains surviving visitors and initializes newly added IDs from the seed.
func (s *Simulation) Resize(count int) {
	count = max(0, min(runtime.MaxNPCCount, count))
	if count < len(s.actors) {
		s.actors = s.actors[:count]
	}
	for i := len(s.actors); i < count; i++ {
		r := s.plan.routes[i]
		n := actor{route: r, forward: true, state: State{ID: "npc_" + strconv.FormatInt(int64(i/10), 10) + strconv.FormatInt(int64(i%10), 10), Position: r.from, State: "walking", SpeedMPS: .6, Phase: math.Mod(float64(i)*.5, 2*math.Pi)}}
		if r.from == r.to {
			n.state.State = "idle"
			n.state.SpeedMPS = 0
		} else {
			n.state.Yaw = facing(r.from, r.to)
		}
		s.actors = append(s.actors, n)
	}
}

// States returns detached storage, including a non-null empty list at count zero.
func (s *Simulation) States() []State {
	out := make([]State, len(s.actors))
	for i := range s.actors {
		out[i] = s.actors[i].state
	}
	return out
}

// Chat accepts only a successfully published text message from this room.
// Equal timestamps select the lowest user ID, irrespective of callback order.
func (s *Simulation) Chat(now time.Time, visitor Visitor) {
	for i := range s.actors {
		a := &s.actors[i]
		if distance(a.state.Position, visitor.Position) > 3 {
			continue
		}
		if a.hasSpeaker && (now.Before(a.spokenAt) || (now.Equal(a.spokenAt) && visitor.UserID >= a.speaker.UserID)) {
			continue
		}
		a.speaker = visitor
		a.spokenAt = now
		a.until = now.Add(3 * time.Second)
		a.hasSpeaker = true
	}
}

// Tick advances at most one 200ms step; scheduler stalls never cause teleportation.
func (s *Simulation) Tick(now time.Time, seconds float64, visitors []Visitor) {
	seconds = max(0, min(.2, seconds))
	for i := range s.actors {
		a := &s.actors[i]
		a.state.SpeedMPS = 0
		if a.hasSpeaker && now.Before(a.until) {
			for _, v := range visitors {
				if v.UserID == a.speaker.UserID {
					a.speaker.Position = v.Position
					break
				}
			}
			a.state.State = "talking"
			if distance(a.state.Position, a.speaker.Position) > 0 {
				a.state.Yaw = facing(a.state.Position, a.speaker.Position)
			}
			continue
		}
		a.state.State = "idle"
		blocked := false
		for _, v := range visitors {
			if distance(a.state.Position, v.Position) < 1 {
				blocked = true
				break
			}
		}
		if blocked || a.route.from == a.route.to {
			continue
		}
		a.state.State = "walking"
		a.state.SpeedMPS = .6
		budget := .6 * seconds
		travelled := 0.0
		for budget > 0 {
			target := a.route.to
			if !a.forward {
				target = a.route.from
			}
			remaining := distance(a.state.Position, target)
			step := min(remaining, budget)
			a.state.Yaw = facing(a.state.Position, target)
			if remaining > 0 {
				a.state.Position.X += (target.X - a.state.Position.X) * step / remaining
				a.state.Position.Z += (target.Z - a.state.Position.Z) * step / remaining
			}
			budget -= step
			travelled += step
			if step == remaining {
				a.forward = !a.forward
			}
		}
		a.state.Phase = math.Mod(a.state.Phase+travelled*2*math.Pi, 2*math.Pi)
	}
}

func distance(a, b player.Vector) float64 { return math.Hypot(a.X-b.X, a.Z-b.Z) }
func facing(from, to player.Vector) float64 {
	yaw := math.Atan2(from.X-to.X, from.Z-to.Z)
	if yaw >= math.Pi {
		yaw -= 2 * math.Pi
	}
	return yaw
}
