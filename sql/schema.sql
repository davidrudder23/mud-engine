-- Player persistence schema.
--
-- Rooms/items/doorways are static, authored content and live in
-- world/rooms/*.json (see internal/world/loader.go). Players are dynamic,
-- multi-user state, so they live here instead.
--
-- current_room_id is plain text, not a foreign key, because room
-- definitions live outside the database.
--
-- id defaults to uuidv7() (time-ordered UUIDs, built into Postgres 18+)
-- rather than a serial int, since a multiplayer system will eventually
-- want ids that can be generated anywhere without a shared counter.

CREATE TABLE IF NOT EXISTS players (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    name TEXT NOT NULL UNIQUE,
    current_room_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
