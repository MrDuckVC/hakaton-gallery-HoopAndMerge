-- +goose Up
CREATE TABLE photos (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    user_id uuid NOT NULL REFERENCES users(id),
    idempotency_key uuid NOT NULL CHECK (substring(idempotency_key::text,15,1)='4' AND substring(idempotency_key::text,20,1) IN ('8','9','a','b')),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint)=32),
    world_version text NOT NULL CHECK (world_version ~ '^[!-~]{1,128}$'),
    room_id text NOT NULL CHECK (room_id ~ '^[!-~]{1,128}$'),
    movement_epoch bigint NOT NULL CHECK (movement_epoch BETWEEN 1 AND 9007199254740991),
    exhibit_id text CHECK (exhibit_id ~ '^[!-~]{1,128}$'),
    caption text NOT NULL CHECK (char_length(caption)<=200),
    published boolean NOT NULL DEFAULT false,
    content_type text NOT NULL CHECK (content_type IN ('image/jpeg','image/webp')),
    original_path text NOT NULL CHECK (original_path=id||'/image'),
    thumbnail_path text NOT NULL CHECK (thumbnail_path=id||'/thumbnail.jpg'),
    image_bytes bigint NOT NULL CHECK (image_bytes BETWEEN 1 AND 2097152),
    thumbnail_bytes bigint NOT NULL CHECK (thumbnail_bytes BETWEEN 1 AND 131072),
    width integer NOT NULL CHECK (width BETWEEN 1 AND 1920),
    height integer NOT NULL CHECK (height BETWEEN 1 AND 1080),
    created_at timestamptz NOT NULL,
    UNIQUE(user_id,idempotency_key)
);
CREATE INDEX photos_album_idx ON photos(user_id,created_at DESC,id DESC);
ALTER TABLE memory_events ADD CONSTRAINT memory_events_photo_fk FOREIGN KEY(photo_id) REFERENCES photos(id);
ALTER TABLE memory_events ADD CONSTRAINT memory_events_photo_created_check CHECK(type<>'PHOTO_CREATED' OR photo_id IS NOT NULL);
CREATE UNIQUE INDEX memory_events_photo_idx ON memory_events(photo_id) WHERE type='PHOTO_CREATED';
