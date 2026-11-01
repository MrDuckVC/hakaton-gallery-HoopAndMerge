-- +goose Up
CREATE TABLE guide_overrides (
    world_version text NOT NULL CHECK (world_version ~ '^[!-~]{1,128}$'),
    guide_id text NOT NULL CHECK (guide_id ~ '^[!-~]{1,128}$'),
    revision bigint NOT NULL CHECK (revision BETWEEN 2 AND 9007199254740991),
    settings jsonb NOT NULL CHECK (jsonb_typeof(settings) = 'object'),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (world_version, guide_id)
);
