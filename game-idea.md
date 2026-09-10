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
- On connect: a random uuid session is created and placed in a random
  room; from then on every line the client sends is run through the
  CommandRunner and the result written back. See `command_structure.md`.
- Still no login/identity — a `Session` (see `mud/session.py`) is not a
  Player and isn't tied to a Player row or placed into `World`.

## Commands

- Design spec: `command_structure.md`.
- `Command` (`mud/commands/command.py`): `match_command(line) -> bool`
  checks fit without running anything; `run_command(line, session_id) ->
  str` runs it. (The spec's separate "execute(session_id)" idea got
  folded into `run_command` taking both the line and the session id,
  rather than having two methods that both mean "run this".)
- `CommandRunner` (`mud/commands/runner.py`): tries each command's
  `match_command` in order, runs the first match, else returns an
  "Unknown command" string.
- Implemented: `look` (re-describes the session's current room —
  needs `SessionRegistry` to know which room that is) and `go` (matches
  `go ...` but is a stub — returns "lorem ipsum"; real movement needs
  direction/doorway parsing and `World.move_player`, and that wants an
  actual Player behind the session).

## Explicitly out of scope for now

- No theme/setting yet — this is just the engine/world model.
- Only `look` and a `go` stub exist; no real movement, inventory, or
  other verbs yet.
- No auth/sessions yet in the Player sense — a telnet `Session` is
  anonymous; it isn't tied to a Player row.

## Status

- [x] Define JSON schema for rooms/items/doorways
- [x] Write loader that reads the JSON world data into in-memory objects
- [x] Player model + Postgres persistence behind a repository interface
- [x] Room <-> player occupancy sync (`World.place_player`/`move_player`)
- [x] Telnet server: connect, see a random room, connection stays open
- [x] Command framework (`Command`, `CommandRunner`) + `look` + `go` stub
- [ ] (next) real `go` (parse direction, use a doorway, `World.move_player`)
- [ ] (later) tie a session to a Player (auth/login)
- [ ] (later) theme/content
