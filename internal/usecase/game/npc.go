package game

import (
	"fmt"
	"strconv"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/npc"
	runtime "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// NPCSnapshot is a complete detached room simulation, converted to JSON by WS.
type NPCSnapshot struct {
	WorldVersion, RoomID, InstanceID string
	Seq                              int64
	NPCs                             []npc.State
}

type npcPlan struct {
	version string
	plan    npc.Plan
}
type npcRoom struct {
	simulation *npc.Simulation
	snapshot   NPCSnapshot
	lastTick   time.Time
}

// ConfigureNPCs validates immutable routes before admission, outside the game lock.
func (s *Service) ConfigureNPCs(rooms []world.Room, newID func() string) error {
	plans := make(map[string]npcPlan, len(rooms))
	for _, room := range rooms {
		plan, err := npc.Prepare(room)
		if err != nil {
			return fmt.Errorf("prepare NPC room %q: %w", room.ID, err)
		}
		plans[room.ID] = npcPlan{version: room.Version, plan: plan}
	}
	s.npcPlans = plans
	s.npcRooms = make(map[string]*npcRoom)
	s.newNPCInstanceID = newID
	return nil
}

func (s *Service) npcSnapshotLocked(room runtime.Room) (*NPCSnapshot, bool) {
	plan, ok := s.npcPlans[room.RoomID]
	if !ok || plan.version != room.WorldVersion {
		return nil, true
	}
	active := s.npcRooms[room.RoomID]
	if active == nil {
		s.npcInstanceSequence++
		active = &npcRoom{simulation: npc.New(plan.plan, room.Config.NPCCount), lastTick: s.now(), snapshot: NPCSnapshot{WorldVersion: room.WorldVersion, RoomID: room.RoomID, InstanceID: s.newNPCInstanceID() + "_" + strconv.FormatUint(s.npcInstanceSequence, 10)}}
		s.npcRooms[room.RoomID] = active
	}
	active.simulation.Resize(room.Config.NPCCount)
	snapshot := active.nextSnapshot()
	if snapshot == nil {
		s.expireNPCLocked(room.RoomID)
		return nil, false
	}
	return snapshot, true
}

func (r *npcRoom) nextSnapshot() *NPCSnapshot {
	if r.snapshot.Seq >= world.MaxSafeInteger {
		return nil
	}
	r.snapshot.Seq++
	r.snapshot.NPCs = r.simulation.States()
	snapshot := r.snapshot
	return &snapshot
}

func (s *Service) enqueueNPCLocked(sink Sink, snapshot *NPCSnapshot, requestID string) bool {
	return snapshot == nil || sink.Enqueue(Event{Type: "npc.state", RequestID: requestID, NPCs: *snapshot})
}

// pruneNPCLocked runs on ownership changes, before any durable cleanup can block.
func (s *Service) pruneNPCLocked() {
	for room := range s.npcRooms {
		occupied := false
		if !s.stopping {
			for _, c := range s.active {
				if !c.ended && c.identity.State.RoomID == room {
					occupied = true
					break
				}
			}
		}
		if !occupied {
			delete(s.npcRooms, room)
		}
	}
}

// endLocked revokes ownership and frees now-empty simulations atomically.
func (s *Service) endLocked(c *connection) {
	c.ended = true
	s.pruneNPCLocked()
}

// PublishNPCs performs bounded memory work only, at most once per 200ms per room.
// No runtime, SQL, filesystem or LLM port is invoked from this path.
func (s *Service) PublishNPCs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	now := s.now()
	for room, r := range s.npcRooms {
		if now.Sub(r.lastTick) < 200*time.Millisecond {
			continue
		}
		visitors := make([]npc.Visitor, 0, len(s.active))
		for user, c := range s.active {
			if !c.ended && c.identity.State.RoomID == room && c.identity.State.WorldVersion == r.snapshot.WorldVersion {
				visitors = append(visitors, npc.Visitor{UserID: user, Position: c.identity.State.Position})
			}
		}
		if len(visitors) == 0 {
			delete(s.npcRooms, room)
			continue
		}

		r.lastTick = r.lastTick.Add((now.Sub(r.lastTick) / (200 * time.Millisecond)) * (200 * time.Millisecond))
		r.simulation.Tick(now, .2, visitors)
		snapshot := r.nextSnapshot()
		if snapshot == nil {
			s.expireNPCLocked(room)
			continue
		}
		for _, c := range s.active {
			if !c.ended && c.identity.State.RoomID == room && c.identity.State.WorldVersion == snapshot.WorldVersion {
				s.enqueueNPCLocked(c.sink, snapshot, "")
			}
		}
	}
}

func (s *Service) npcChatLocked(c *connection) {
	if r := s.npcRooms[c.identity.State.RoomID]; r != nil {
		r.simulation.Chat(s.now(), npc.Visitor{UserID: c.identity.User.ID, Position: c.identity.State.Position})
	}
}

// expireNPCLocked closes owners before a sequence can leave JSON safe integers.
func (s *Service) expireNPCLocked(roomID string) {
	for _, c := range s.active {
		if !c.ended && c.identity.State.RoomID == roomID {
			s.endLocked(c)
			c.sink.Close(1012)
			s.markLampDirtyLocked(roomID)
		}
	}
	delete(s.npcRooms, roomID)
}
