package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/security"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
	"github.com/jackc/pgx/v5/pgxpool"
)

func socialSession(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) domain.Session {
	t.Helper()
	crypto := security.Crypto{}
	s := domain.Session{ID: crypto.NewID(), UserID: crypto.NewID()}
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,display_name,avatar_color,password_hash) VALUES($1,$2,$2,'#AABBCC','encoded')`, s.UserID, name); err != nil {
		t.Fatal(err)
	}
	_, hash, err := crypto.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,expires_at) VALUES($1,$2,$3,now()+interval '1 day')`, s.ID, s.UserID, hash[:]); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestInspectionMemoryAtomicRepeatIsolationIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewSocialStore(pool)
	a := socialSession(ctx, t, pool, "alice")
	b := socialSession(ctx, t, pool, "bob")
	state := player.State{WorldVersion: "v1", RoomID: "room"}
	first, err := store.RecordInspection(ctx, a, state, "ex")
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListMemoryEvents(ctx, a.UserID, 10)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	eventID := events[0].ID
	if events[0].Type != "EXHIBIT_INSPECTED" || events[0].ExhibitID == nil || *events[0].ExhibitID != "ex" || !events[0].CreatedAt.Equal(first) {
		t.Fatal(events)
	}
	state.WorldVersion = "v2"
	state.RoomID = "new-room"
	second, err := store.RecordInspection(ctx, a, state, "ex")
	if err != nil || !second.After(first) {
		t.Fatal(first, second, err)
	}
	events, err = store.ListMemoryEvents(ctx, a.UserID, 10)
	if err != nil || len(events) != 1 || events[0].ID != eventID || events[0].WorldVersion != "v2" || events[0].RoomID != "new-room" || !events[0].CreatedAt.Equal(second) {
		t.Fatal(events, err)
	}
	var version, room string
	var inspected time.Time
	if err = pool.QueryRow(ctx, `SELECT world_version,room_id,inspected_at FROM exhibit_inspections WHERE user_id=$1 AND exhibit_id='ex'`, a.UserID).Scan(&version, &room, &inspected); err != nil || version != "v2" || room != "new-room" || !inspected.Equal(second) {
		t.Fatal(version, room, inspected, err)
	}
	if events, err = store.ListMemoryEvents(ctx, b.UserID, 10); err != nil || len(events) != 0 {
		t.Fatal("foreign memory", events, err)
	}
	if _, err = store.RecordInspection(ctx, b, state, "ex"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RecordInspection(ctx, a, state, "second"); err != nil {
		t.Fatal(err)
	}
	events, err = store.ListMemoryEvents(ctx, a.UserID, 1)
	if err != nil || len(events) != 1 || *events[0].ExhibitID != "second" {
		t.Fatal("bounded newest first", events, err)
	}
	for _, limit := range []int{0, 101} {
		if _, err = store.ListMemoryEvents(ctx, a.UserID, limit); !errors.Is(err, domain.Validation) {
			t.Fatal("invalid limit", err)
		}
	}
	// A failure inserting memory must also undo the inspection insert/update.
	if _, err = pool.Exec(ctx, `ALTER TABLE memory_events ADD CONSTRAINT reject_test_room CHECK(room_id <> 'reject')`); err != nil {
		t.Fatal(err)
	}
	state.RoomID = "reject"
	for _, ex := range []string{"ex", "rejected-new"} {
		if _, err = store.RecordInspection(ctx, a, state, ex); err == nil {
			t.Fatal("constraint failure accepted")
		}
	}
	events, err = store.ListMemoryEvents(ctx, a.UserID, 10)
	if err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
	if err = pool.QueryRow(ctx, `SELECT inspected_at FROM exhibit_inspections WHERE user_id=$1 AND exhibit_id='ex'`, a.UserID).Scan(&inspected); err != nil || !inspected.Equal(second) {
		t.Fatal("partial update", inspected, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM exhibit_inspections WHERE exhibit_id='rejected-new'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial insert", count, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	state.RoomID = "room"
	if _, err = store.RecordInspection(ctx, a, state, "revoked"); !errors.Is(err, domain.Unauthenticated) {
		t.Fatal(err)
	}
	// An otherwise valid session cannot be reused under another owner.
	b.UserID = a.UserID
	if _, err = store.RecordInspection(ctx, b, state, "forged"); !errors.Is(err, domain.Unauthenticated) {
		t.Fatal(err)
	}
}
func TestInspectionConcurrentRepeatIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewSocialStore(pool)
	session := socialSession(ctx, t, pool, "alice")
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, err := store.RecordInspection(ctx, session, player.State{WorldVersion: "v1", RoomID: "room"}, "ex"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	events, err := store.ListMemoryEvents(ctx, session.UserID, 100)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	var count int
	var inspected time.Time
	if err = pool.QueryRow(ctx, `SELECT count(*),max(inspected_at) FROM exhibit_inspections WHERE user_id=$1`, session.UserID).Scan(&count, &inspected); err != nil || count != 1 || !inspected.Equal(events[0].CreatedAt) {
		t.Fatal(count, inspected, events, err)
	}
}
