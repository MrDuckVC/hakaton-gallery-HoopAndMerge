-- +goose Up
CREATE TABLE users (
    id uuid PRIMARY KEY,
    username text NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9_]{3,32}$'),
    display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 32),
    avatar_color text NOT NULL CHECK (avatar_color ~ '^#[0-9A-Fa-f]{6}$'),
    password_hash text NOT NULL CHECK (password_hash <> ''),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    user_id uuid NOT NULL REFERENCES users(id),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);

CREATE TABLE player_states (
    user_id uuid PRIMARY KEY REFERENCES users(id),
    world_version text NOT NULL CHECK (world_version ~ '^[!-~]{1,128}$'),
    room_id text NOT NULL CHECK (room_id ~ '^[!-~]{1,128}$'),
    x double precision NOT NULL CHECK (x > '-Infinity'::double precision AND x < 'Infinity'::double precision),
    y double precision NOT NULL CHECK (y = 0),
    z double precision NOT NULL CHECK (z > '-Infinity'::double precision AND z < 'Infinity'::double precision),
    yaw double precision NOT NULL CHECK (yaw >= -pi() AND yaw < pi()),
    pitch double precision NOT NULL CHECK (pitch >= -pi()/2 AND pitch <= pi()/2),
    updated_at timestamptz NOT NULL DEFAULT now()
);
