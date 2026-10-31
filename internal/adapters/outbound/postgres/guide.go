package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	usecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GuideStore provides serialized admission and transactional completion with memory.
type GuideStore struct{ pool *pgxpool.Pool }

// NewGuideStore uses the process-owned connection pool.
func NewGuideStore(pool *pgxpool.Pool) *GuideStore { return &GuideStore{pool} }

const generationColumns = `id::text,user_id::text,request_correlation_id,guide_id,COALESCE(exhibit_id,''),normalized_payload_hash,status,user_text,user_message_id::text,user_created_at,assistant_text,assistant_message_id::text,assistant_created_at,error_code,error_message,error_retryable,created_at,started_at,finished_at,origin_connection_id`

func scanGeneration(row pgx.Row) (domain.Generation, error) {
	var g domain.Generation
	var text, id, code, message *string
	var created *time.Time
	var retry *bool
	err := row.Scan(&g.ID, &g.UserID, &g.RequestID, &g.GuideID, &g.ExhibitID, &g.Fingerprint, &g.Status, &g.UserMessage.Text, &g.UserMessage.ID, &g.UserMessage.CreatedAt, &text, &id, &created, &code, &message, &retry, &g.CreatedAt, &g.StartedAt, &g.FinishedAt, &g.ConnectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, auth.Code("NOT_FOUND")
	}
	if err != nil {
		return g, auth.Unavailable
	}
	if text != nil && id != nil && created != nil {
		g.AssistantMessage = &domain.Message{ID: *id, Text: *text, CreatedAt: *created}
	}
	if code != nil && message != nil && retry != nil {
		g.Error = &domain.Error{Code: *code, Message: *message, Retryable: *retry}
	}
	return g, nil
}

type guideTx struct{ pgx.Tx }

func (s *GuideStore) locked(ctx context.Context) (*guideTx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, auth.Unavailable
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM guide_scheduler WHERE id=1 FOR UPDATE`); err != nil {
		_ = tx.Rollback(ctx)
		return nil, auth.Unavailable
	}
	return &guideTx{tx}, nil
}

// ByRequest scopes durable idempotency to the authenticated user.
func (s *GuideStore) ByRequest(ctx context.Context, user, request string) (domain.Generation, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	return scanGeneration(s.pool.QueryRow(ctx, `SELECT `+generationColumns+` FROM guide_generations WHERE user_id=$1 AND request_id::text=lower($2)`, user, request))
}

// Create serializes key replay, busy and capacity checks with the queue insertion.
func (s *GuideStore) Create(ctx context.Context, session auth.Session, g domain.Generation) (domain.Generation, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := s.locked(ctx)
	if err != nil {
		return domain.Generation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockRepairSession(ctx, tx, session); err != nil {
		return domain.Generation{}, err
	}
	old, err := scanGeneration(tx.QueryRow(ctx, `SELECT `+generationColumns+` FROM guide_generations WHERE user_id=$1 AND request_id::text=lower($2)`, session.UserID, g.RequestID))
	if err == nil {
		if old.Fingerprint != g.Fingerprint {
			return old, auth.Code("IDEMPOTENCY_CONFLICT")
		}
		return old, nil
	}
	if !errors.Is(err, auth.Code("NOT_FOUND")) {
		return old, err
	}
	var busy bool
	var queued int
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM guide_generations WHERE user_id=$1 AND status IN ('queued','running')),(SELECT count(*) FROM guide_generations WHERE status='queued')`, session.UserID).Scan(&busy, &queued); err != nil {
		return g, auth.Unavailable
	}
	if busy {
		return g, auth.Code("GUIDE_BUSY")
	}
	if queued >= 8 {
		return g, auth.Code("GUIDE_QUEUE_FULL")
	}
	g.UserID = session.UserID
	g, err = scanGeneration(tx.QueryRow(ctx, `INSERT INTO guide_generations(id,user_id,request_id,request_correlation_id,guide_id,exhibit_id,normalized_payload_hash,status,user_text,user_message_id,user_created_at,created_at,origin_connection_id) VALUES($1,$2,$3,$11,$4,NULLIF($5,''),$6,'queued',$7,$8,$9,$9,$10) RETURNING `+generationColumns, g.ID, g.UserID, g.RequestID, g.GuideID, g.ExhibitID, g.Fingerprint, g.UserMessage.Text, g.UserMessage.ID, g.CreatedAt, g.ConnectionID, g.RequestID))
	if err != nil {
		return g, auth.Unavailable
	}
	if err = lockRepairSession(ctx, tx, session); err != nil {
		return g, err
	}
	if err = tx.Commit(ctx); err != nil {
		return g, auth.Unavailable
	}
	return g, nil
}

// Claim reserves the oldest queued record only when no other record is running.
func (s *GuideStore) Claim(ctx context.Context, now time.Time) (domain.Generation, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := s.locked(ctx)
	if err != nil {
		return domain.Generation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	g, err := scanGeneration(tx.QueryRow(ctx, `UPDATE guide_generations SET status='running',started_at=$1 WHERE id=(SELECT id FROM guide_generations WHERE status='queued' AND created_at>$1::timestamptz-interval '120 seconds' AND NOT EXISTS(SELECT 1 FROM guide_generations WHERE status='running') ORDER BY created_at,id LIMIT 1) RETURNING `+generationColumns, now))
	if err != nil {
		return g, err
	}
	if err = tx.Commit(ctx); err != nil {
		return g, auth.Unavailable
	}
	return g, nil
}

// Pending returns queue entries for cancellation and deadline enforcement.
func (s *GuideStore) Pending(ctx context.Context) ([]domain.Generation, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT `+generationColumns+` FROM guide_generations WHERE status IN ('queued','running') ORDER BY created_at,id`)
	if err != nil {
		return nil, auth.Unavailable
	}
	defer rows.Close()
	result := make([]domain.Generation, 0, 9)
	for rows.Next() {
		g, e := scanGeneration(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, g)
	}
	if rows.Err() != nil {
		return nil, auth.Unavailable
	}
	return result, nil
}

// Finish elects a single terminal winner and commits completed memory with the answer.
func (s *GuideStore) Finish(ctx context.Context, g domain.Generation) (domain.Generation, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	if !g.Terminal() || g.FinishedAt == nil {
		return domain.Generation{}, false, auth.Validation
	}
	tx, err := s.locked(ctx)
	if err != nil {
		return g, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var text, id any
	var created any
	var code, message, retry any
	if g.AssistantMessage != nil {
		text = g.AssistantMessage.Text
		id = g.AssistantMessage.ID
		created = g.AssistantMessage.CreatedAt
	}
	if g.Error != nil {
		code = g.Error.Code
		message = g.Error.Message
		retry = g.Error.Retryable
	}
	current, err := scanGeneration(tx.QueryRow(ctx, `UPDATE guide_generations SET status=$3,assistant_text=$4,assistant_message_id=$5,assistant_created_at=$6,error_code=$7,error_message=$8,error_retryable=$9,finished_at=$10 WHERE user_id=$1 AND id::text=$2 AND status IN ('queued','running') RETURNING `+generationColumns, g.UserID, g.ID, g.Status, text, id, created, code, message, retry, g.FinishedAt))
	if errors.Is(err, auth.Code("NOT_FOUND")) {
		current, err = scanGeneration(tx.QueryRow(ctx, `SELECT `+generationColumns+` FROM guide_generations WHERE user_id=$1 AND id::text=$2`, g.UserID, g.ID))
		return current, false, err
	}
	if err != nil {
		return g, false, err
	}
	if current.Status == "completed" {
		items, readErr := completedContext(ctx, tx, current.UserID, current.GuideID)
		if readErr != nil {
			return g, false, readErr
		}
		summary := usecase.Summarize(items)
		_, err = tx.Exec(ctx, `INSERT INTO guide_memories(user_id,guide_id,summary,version,updated_at) VALUES($1,$2,$3,1,$4) ON CONFLICT(user_id,guide_id) DO UPDATE SET summary=EXCLUDED.summary,version=guide_memories.version+1,updated_at=EXCLUDED.updated_at`, current.UserID, current.GuideID, summary, g.FinishedAt)
		if err != nil {
			return g, false, auth.Unavailable
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return g, false, auth.Unavailable
	}
	return current, true, nil
}

// Get hides unknown and foreign IDs alike.
func (s *GuideStore) Get(ctx context.Context, user, id string) (domain.Generation, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	return scanGeneration(s.pool.QueryRow(ctx, `SELECT `+generationColumns+` FROM guide_generations WHERE user_id=$1 AND id::text=$2`, user, id))
}

// History validates a durable boundary against its owner and guide before keyset pagination.
func (s *GuideStore) History(ctx context.Context, user, guide string, limit int, before string) ([]domain.Generation, string, error) {
	if limit < 1 || limit > 50 {
		return nil, "", auth.Validation
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	var boundaryTime any
	var boundaryID any
	if before != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(before)
		if err != nil || len(decoded) != 36 {
			return nil, "", auth.Code("INVALID_CURSOR")
		}
		g, err := s.Get(ctx, user, string(decoded))
		if err != nil {
			if errors.Is(err, auth.Code("NOT_FOUND")) {
				return nil, "", auth.Code("INVALID_CURSOR")
			}
			return nil, "", err
		}
		if g.GuideID != guide {
			return nil, "", auth.Code("INVALID_CURSOR")
		}
		boundaryTime = g.CreatedAt
		boundaryID = g.ID
	}
	rows, err := s.pool.Query(ctx, `SELECT `+generationColumns+` FROM guide_generations WHERE user_id=$1 AND guide_id=$2 AND ($3::timestamptz IS NULL OR (created_at,id)<($3::timestamptz,$4::uuid)) ORDER BY created_at DESC,id DESC LIMIT $5`, user, guide, boundaryTime, boundaryID, limit+1)
	if err != nil {
		return nil, "", auth.Unavailable
	}
	defer rows.Close()
	items := make([]domain.Generation, 0, limit+1)
	for rows.Next() {
		g, e := scanGeneration(rows)
		if e != nil {
			return nil, "", e
		}
		items = append(items, g)
	}
	if rows.Err() != nil {
		return nil, "", auth.Unavailable
	}
	cursor := ""
	if len(items) > limit {
		items = items[:limit]
		cursor = base64.RawURLEncoding.EncodeToString([]byte(items[len(items)-1].ID))
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, cursor, nil
}

// Recover makes restart/outage recovery durable before admission resumes.
func (s *GuideStore) Recover(ctx context.Context, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := s.locked(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	failure := domain.Failure("SERVER_RESTARTED")
	_, err = tx.Exec(ctx, `UPDATE guide_generations SET status='failed',error_code=$1,error_message=$2,error_retryable=$3,finished_at=$4 WHERE status IN ('queued','running')`, failure.Code, failure.Message, failure.Retryable, now)
	if err != nil {
		return fmt.Errorf("recover generations: %w", auth.Unavailable)
	}
	if err = tx.Commit(ctx); err != nil {
		return auth.Unavailable
	}
	return nil
}

var _ usecase.Store = (*GuideStore)(nil)
