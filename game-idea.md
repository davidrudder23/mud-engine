# MUD Engine — Idea

## Goal

An experiment in agentic development: build the *engine* for a text-based
MUD (Multi-User Dungeon), theme-agnostic, focused on the world model rather
than gameplay verbs.

## Core concepts

- **Structure**: a building, cavern, or other container that groups
  rooms together (e.g. "Old Mill"). Has a `name` and `description`.
  Every room belongs to exactly one structure; a structure can contain
  many rooms.
- **Room**: a location the player's character can be in. Belongs to
  exactly one structure.
  - Has metadata, e.g. a `description`.
  - Has **items** (contents).
  - Has **doorways** connecting it to other rooms.
  - `Room.format()` lists current occupants ("Players here: ...") too —
    including the viewer themselves, since `format()` has no notion of
    who's asking. This is where the duplicate-login quirk (see
    Networking below) actually becomes visible in play, not just in
    the data model.
- **Item**: an object that lives in a room (and presumably can move between
  rooms / into inventory later, but that's not in scope yet).
- **Doorway**: a connection between two rooms.

## Data storage

- Structure/room (and related) metadata lives in **JSON files** — the
  world is authored as data, not code. `src/world/structures/*.json` and
  `src/world/rooms/*.json` (see `src/mud/loader.py`).
- Structure <-> room membership is kept in sync the same way as room
  <-> player occupancy (see below): `World.place_room` /
  `World.move_room_to_structure` are the only things that should touch
  `Structure.rooms` or `Room.structure_id`. Unlike player occupancy,
  this relationship is static content, established once at load time —
  `move_room_to_structure` exists for symmetry/future use (e.g. a level
  editor) but nothing currently calls it after loading.

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
  / `World.move_player` / `World.remove_player` — nothing else should
  touch `Room.players` directly. See `src/demo_world_players.py`.
- **Location lives on the Player, not the Session.** A `Session` (see
  `src/mud/session.py`) has a `player`, one-way — a session knows its
  player, but a player doesn't know its session(s). That's deliberate:
  the same player *name* can be behind more than one session at once
  (see Networking below), and there's no reverse list to keep in sync.
- **`Player` uses identity equality (`@dataclass(eq=False)`), not
  value equality.** Two Player objects loaded from the same DB row
  (e.g. two logins under the same name) are never `==` to each other,
  only to themselves. This is load-bearing, not incidental: without it,
  `World.place_player`'s `if player not in room.players` check would
  treat a second login as "already here" and silently drop it instead
  of adding a second occupant.

## Networking

- A telnet server (`src/server.py` / `src/mud/telnet_server.py`), port 8023 by
  default, since this is meant to feel like an old-school MUD.
- On connect: prompts "What is your name?", then `PlayerRepository.get_by_name`
  (or `create`, if new) gets/creates a Player, `World.place_player` adds
  it to its room's occupant list, and a `Session` is created around it.
  Every line the client sends after that goes through the CommandRunner.
- **Logging in twice under the same name is allowed, on purpose**, and
  each login gets its own `Session` + its own `Player` *instance*
  (same underlying DB row, per `get_by_name`) — see the identity-equality
  note above. The two move independently and can both show up as
  occupants of the same room at once: one player, apparently in two
  places, moving on its own in each. That's the intended (if delightfully
  weird) consequence of the design, not a bug to fix.
- Still no real auth — whatever name you type, you get (or become)
  that player. No password, no limit on concurrent sessions per name.

## Commands

- Design spec: `command_structure.md`.
- `Command` (`src/mud/commands/command.py`): `match_command(line) -> bool`
  checks fit without running anything; `run_command(line, session_id) ->
  str` runs it. (The spec's separate "execute(session_id)" idea got
  folded into `run_command` taking both the line and the session id,
  rather than having two methods that both mean "run this".)
- `CommandRunner` (`src/mud/commands/runner.py`): tries each command's
  `match_command` in order, runs the first match, else returns an
  "Unknown command" string.
- Matching (`matches_verb` in `src/mud/commands/command.py`): case-insensitive
  prefix check against the verb, since commands can take parameters
  ("go east", "look around"). It's a plain prefix, not word-boundary, so
  e.g. "goblin" also matches "go" — accepted tradeoff, not a bug.
- Implemented:
  - `look` — re-describes `session.player`'s current room (via
    `World.get_room(session.player.current_room_id)`).
  - `go <dimension>` — moves through a doorway. Looks up the player's
    current room's doorways for an exact, case-sensitive match on
    `direction` ("go North" won't match a doorway whose direction is
    "north"); on a match, calls `World.move_player` (updates
    `Room.players` on both ends, not just `current_room_id`) and
    persists the move via `PlayerRepository.save`. No match, or no
    argument, gets an error string.
  - `quit` — closes the connection. Commands only return text (no
    access to the writer), so it works by setting `should_close` on the
    `Session`; telnet_server's read loop checks that flag after every
    command and closes if it's set.

## Explicitly out of scope for now

- No theme/setting yet — this is just the engine/world model.
- Only `look`, `go`, and `quit` exist; no inventory or other verbs yet.
- No real auth — logging in is just typing a name; no password, no
  identity verification, no limit on concurrent sessions per name (see
  Networking above — that last one is deliberate, not a gap).

## Status

- [x] Define JSON schema for rooms/items/doorways
- [x] Write loader that reads the JSON world data into in-memory objects
- [x] Player model + Postgres persistence behind a repository interface
- [x] Room <-> player occupancy sync (`World.place_player`/`move_player`/`remove_player`)
- [x] Telnet server: connect, see a random room, connection stays open
- [x] Command framework (`Command`, `CommandRunner`) + `look` + `go` + `quit`
- [x] Login (name -> Player), Session tied to Player (not Room);
      `go`/`look` operate on the player, persisted via `PlayerRepository`
- [ ] (later) real auth (passwords, etc.) if that ever matters here
- [ ] (later) theme/content
