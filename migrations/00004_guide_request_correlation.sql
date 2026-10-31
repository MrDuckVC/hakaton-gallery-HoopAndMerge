-- +goose Up
-- Preserve event correlation separately from PostgreSQL's canonical UUID spelling.
ALTER TABLE guide_generations ADD COLUMN request_correlation_id text;
UPDATE guide_generations SET request_correlation_id=request_id::text;
ALTER TABLE guide_generations ALTER COLUMN request_correlation_id SET NOT NULL;
ALTER TABLE guide_generations ADD CONSTRAINT guide_request_correlation_matches
    CHECK (request_correlation_id::uuid = request_id);
