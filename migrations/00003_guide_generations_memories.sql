-- +goose Up
CREATE TABLE guide_scheduler (
    id integer PRIMARY KEY CHECK (id = 1)
);
INSERT INTO guide_scheduler(id) VALUES (1);

CREATE TABLE guide_generations (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    request_id uuid NOT NULL,
    guide_id text NOT NULL CHECK (guide_id ~ '^[!-~]{1,128}$'),
    exhibit_id text CHECK (exhibit_id ~ '^[!-~]{1,128}$'),
    normalized_payload_hash text NOT NULL,
    status text NOT NULL CHECK (status IN ('queued','running','completed','failed')),
    user_text text NOT NULL CHECK (char_length(user_text) BETWEEN 1 AND 2000),
    user_message_id uuid NOT NULL,
    user_created_at timestamptz NOT NULL,
    assistant_text text CHECK (char_length(assistant_text) BETWEEN 0 AND 8000),
    assistant_message_id uuid,
    assistant_created_at timestamptz,
    error_code text,
    error_message text,
    error_retryable boolean,
    created_at timestamptz NOT NULL,
    started_at timestamptz,
    finished_at timestamptz,
    origin_connection_id text NOT NULL,
    UNIQUE(user_id,request_id),
    CHECK (
      (status IN ('queued','running') AND assistant_text IS NULL AND assistant_message_id IS NULL AND assistant_created_at IS NULL AND error_code IS NULL AND error_message IS NULL AND error_retryable IS NULL AND finished_at IS NULL)
      OR (status='completed' AND assistant_text IS NOT NULL AND assistant_message_id IS NOT NULL AND assistant_created_at IS NOT NULL AND error_code IS NULL AND error_message IS NULL AND error_retryable IS NULL AND finished_at IS NOT NULL)
      OR (status='failed' AND assistant_text IS NULL AND assistant_message_id IS NULL AND assistant_created_at IS NULL AND error_code IS NOT NULL AND error_message IS NOT NULL AND error_retryable IS NOT NULL AND finished_at IS NOT NULL)
    )
);
CREATE UNIQUE INDEX guide_one_pending_user ON guide_generations(user_id) WHERE status IN ('queued','running');
CREATE UNIQUE INDEX guide_one_running ON guide_generations((1)) WHERE status='running';
CREATE INDEX guide_history ON guide_generations(user_id,guide_id,created_at,id);
CREATE INDEX guide_queue ON guide_generations(status,created_at,id);

CREATE TABLE guide_memories (
    user_id uuid NOT NULL REFERENCES users(id),
    guide_id text NOT NULL,
    summary text NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY(user_id,guide_id)
);
