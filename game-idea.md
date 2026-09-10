# MUD Engine — Idea

## Goal

An experiment in agentic development: build the *engine* for a text-based
MUD (Multi-User Dungeon), theme-agnostic, focused on the world model rather
than gameplay verbs.

## Core concepts

- **Room**: a location the player's character can be in.
  - Has metadata, e.g. a `description`.
  - Has **items** (contents).
  - Has **doorways** connecting it to other rooms.
- **Item**: an object that lives in a room (and presumably can move between
  rooms / into inventory later, but that's not in scope yet).
- **Doorway**: a connection between two rooms.

## Data storage

- Room (and related) metadata lives in **JSON files** — the world is
  authored as data, not code.

## Players (multiplayer)

- Multiple players can be in the world at once.
- Players are dynamic, per-user state (name, current room), so unlike
  rooms/items they're persisted in a database rather than JSON.
- For now: local PostgreSQL. Built with future scale in mind (pooled
  connections, UUID ids, storage hidden behind a repository interface)
  but kept as simple as that allows — no ORM, no auth, no sessions yet.
- See `README.md` for setup/run instructions.
- Each player is in exactly one room. Each room knows who's in it
  (`Room.players`, in-memory only — occupancy doesn't survive a
  restart, `Player.current_room_id` in Postgres does). The two are kept
  from drifting apart by routing every change through `World.place_player`
  / `World.move_player` — nothing else should touch `Room.players`
  directly. See `demo_world_players.py`.

## Networking

- A telnet server (`server.py` / `mud/telnet_server.py`), port 8023 by
  default, since this is meant to feel like an old-school MUD.
- Current behavior: on connect, show a random room, then hold the
  connection open. No login/identity yet — a connection isn't tied to a
  Player row or placed into `World` via `place_player`.
- No command parsing yet — that's next: read lines from the connection
  and dispatch them, which is also when a connection will need to
  become an actual Player (auth/session, not just an anonymous socket).

## Explicitly out of scope for now

- No theme/setting yet — this is just the engine/world model.
- No player actions/verbs yet (no "go east", no "pick up item", etc.).
  Just the data model and whatever scaffolding is needed to load and
  represent it.
- No auth/sessions yet — telnet connections are anonymous; they aren't
  tied to a Player row.

## Status

- [x] Define JSON schema for rooms/items/doorways
- [x] Write loader that reads the JSON world data into in-memory objects
- [x] Player model + Postgres persistence behind a repository interface
- [x] Room <-> player occupancy sync (`World.place_player`/`move_player`)
- [x] Telnet server: connect, see a random room, connection stays open
- [ ] (next) read + dispatch commands from the telnet connection
- [ ] (later) tie a connection to a Player (auth/session)
- [ ] (later) theme/content
