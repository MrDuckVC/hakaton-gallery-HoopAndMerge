-- +goose Up
CREATE TABLE admin_memberships (
    user_id uuid PRIMARY KEY REFERENCES users(id),
    granted_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE room_overlays (
    world_version text NOT NULL CHECK (world_version ~ '^[!-~]{1,128}$'),
    room_id text NOT NULL CHECK (room_id ~ '^[!-~]{1,128}$'),
    revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991),
    config jsonb NOT NULL CHECK (jsonb_typeof(config) = 'object'),
    lamp jsonb NOT NULL CHECK (jsonb_typeof(lamp) = 'object'),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (world_version, room_id)
);
