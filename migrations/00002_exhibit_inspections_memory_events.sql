-- +goose Up
CREATE TABLE exhibit_inspections (
    user_id uuid NOT NULL REFERENCES users(id),
    exhibit_id text NOT NULL CHECK (exhibit_id ~ '^[!-~]{1,128}$'),
    world_version text NOT NULL CHECK (world_version ~ '^[!-~]{1,128}$'),
    room_id text NOT NULL CHECK (room_id ~ '^[!-~]{1,128}$'),
    inspected_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, exhibit_id)
);

CREATE TABLE memory_events (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    type text NOT NULL CHECK (type <> ''),
    world_version text NOT NULL CHECK (world_version ~ '^[!-~]{1,128}$'),
    room_id text NOT NULL CHECK (room_id ~ '^[!-~]{1,128}$'),
    exhibit_id text CHECK (exhibit_id ~ '^[!-~]{1,128}$'),
    photo_id text CHECK (photo_id ~ '^[!-~]{1,128}$'),
    created_at timestamptz NOT NULL,
    CHECK (type <> 'EXHIBIT_INSPECTED' OR (exhibit_id IS NOT NULL AND photo_id IS NULL))
);
CREATE UNIQUE INDEX memory_events_inspection_idx ON memory_events(user_id, exhibit_id)
    WHERE type = 'EXHIBIT_INSPECTED';
CREATE INDEX memory_events_personal_idx ON memory_events(user_id, created_at DESC, id DESC);
