-- +goose Up
-- Apply with the backend stopped, before selecting gallery_c01_v2.
-- Migration 00008 already normalizes legacy counts of 40 to 10.
-- Preserve source rows, unrelated worlds, all other config fields and lamp state.
INSERT INTO room_overlays (world_version, room_id, revision, config, lamp, updated_at)
SELECT 'gallery_c01_v2', room_id, revision + 1,
       jsonb_set(config, '{npc_count}', '5'::jsonb), lamp, now()
FROM room_overlays
WHERE world_version = 'gallery_c01_v1' AND room_id IN (
    'room_leonardo_portrait',
    'room_leonardo_space',
    'room_leonardo_perspective',
    'room_leonardo_light',
    'room_leonardo_figure',
    'room_leonardo_gesture',
    'room_leonardo_composition',
    'room_leonardo_details',
    'room_monet_morning',
    'room_monet_daylight',
    'room_monet_evening',
    'room_monet_water',
    'room_monet_garden',
    'room_monet_city',
    'room_monet_snow',
    'room_monet_series',
    'room_vangogh_self',
    'room_vangogh_person',
    'room_vangogh_still-life',
    'room_vangogh_village',
    'room_vangogh_field',
    'room_vangogh_trees',
    'room_vangogh_interior',
    'room_vangogh_expression'
)
ON CONFLICT (world_version, room_id) DO NOTHING;

-- Also handle an already prepared target world without replacing its settings.
UPDATE room_overlays
SET config = jsonb_set(config, '{npc_count}', '5'::jsonb),
    revision = revision + 1,
    updated_at = now()
WHERE world_version = 'gallery_c01_v2'
  AND config->'npc_count' IS DISTINCT FROM '5'::jsonb
  AND room_id IN (
    'room_leonardo_portrait',
    'room_leonardo_space',
    'room_leonardo_perspective',
    'room_leonardo_light',
    'room_leonardo_figure',
    'room_leonardo_gesture',
    'room_leonardo_composition',
    'room_leonardo_details',
    'room_monet_morning',
    'room_monet_daylight',
    'room_monet_evening',
    'room_monet_water',
    'room_monet_garden',
    'room_monet_city',
    'room_monet_snow',
    'room_monet_series',
    'room_vangogh_self',
    'room_vangogh_person',
    'room_vangogh_still-life',
    'room_vangogh_village',
    'room_vangogh_field',
    'room_vangogh_trees',
    'room_vangogh_interior',
    'room_vangogh_expression'
);

-- Stable guide/material IDs allow settings to follow this content-only release.
INSERT INTO guide_overrides (world_version, guide_id, revision, settings, updated_at)
SELECT 'gallery_c01_v2', guide_id, revision, settings, updated_at
FROM guide_overrides
WHERE world_version = 'gallery_c01_v1'
  AND guide_id IN ('guide_leonardo', 'guide_monet', 'guide_vangogh')
ON CONFLICT (world_version, guide_id) DO NOTHING;
