-- +goose Up
-- Apply before starting the backend with the new 0..10 visitor limit.
-- Room revision advances atomically with the full runtime config change.
UPDATE room_overlays
SET config = jsonb_set(config, '{npc_count}', '10'::jsonb, false),
    revision = revision + 1,
    updated_at = now()
WHERE (config->>'npc_count')::numeric > 10;
