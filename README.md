# MUD Engine

A text-based multiplayer dungeon engine. Design notes live in
`game-idea.md`.

- Structures/rooms/items/doorways are static, authored **content**,
  loaded from `world/structures/*.json` and `world/rooms/*.json` (see
  `mud/loader.py`). Every room belongs to exactly one structure (a
  building, cavern, etc.). No verbs/actions yet beyond `look`/`go`/`quit`.
- Players are dynamic, multi-user **state**, persisted in Postgres (see
  `mud/repositories/`).

## Setup

Requires PostgreSQL and the `psycopg2` driver. On this machine
PostgreSQL is already installed and a `mud` database exists, owned by
the local OS user, using peer auth (no password). To set this up
elsewhere:

    sudo apt-get install postgresql postgresql-contrib
    sudo systemctl start postgresql
    sudo -u postgres createuser --createdb "$(whoami)"
    sudo -u postgres createdb -O "$(whoami)" mud

For the `psycopg2` driver itself, there are two options, and it matters
which Python you actually run the scripts with:

- **System Python** (plain `python3 ...`, no venv activated): install
  the system package — `sudo apt-get install python3-psycopg2`.
- **This project's venv** (`.venv/`, created with `virtualenv`, so it
  does *not* see system packages): `.venv/bin/pip install -r requirements.txt`.
  Once that's done, either activate it (`source .venv/bin/activate`)
  or just run scripts as `.venv/bin/python3 ...`.

Mixing the two up is exactly what produces
`ModuleNotFoundError: No module named 'psycopg2'` — the driver is
installed for one Python but you ran the scripts with the other.

Then create the schema:

    python3 migrate.py

Connection settings default to a local Unix-socket connection as your
OS user against a database named `mud`. Override with env vars if
needed: `MUD_DB_HOST`, `MUD_DB_PORT`, `MUD_DB_NAME`, `MUD_DB_USER`,
`MUD_DB_PASSWORD`. (Only `MUD_DB_HOST` and `MUD_DB_PASSWORD` need
setting for a typical remote Postgres — the rest have sane defaults.)

## Run the server

    python3 server.py

Then connect with a telnet client:

    telnet localhost 8023

You'll be asked for a name — type anything; there's no password, and
that name either becomes an existing player or creates a new one. Try:

    look        re-describes the room you're in
    go <dir>    moves through a doorway (exact, case-sensitive match, e.g. "go north")
    quit        closes the connection

Anything else gets an "Unknown command" reply. Config via env vars:
`MUD_TELNET_HOST` (default `127.0.0.1`, loopback only — set to
`0.0.0.0` to accept connections from other machines) and
`MUD_TELNET_PORT` (default `8023`).

Logging in twice under the same name is allowed on purpose — you get
two independent sessions of "the same" player, each moving on its own
and both showing up in `Room.players` at once. See `game-idea.md`.

## Try it

    python3 demo.py               # loads world/{structures,rooms}/*.json — no DB needed
    python3 demo_player.py        # creates/moves/looks up a player in Postgres
    python3 demo_world_players.py # room <-> player occupancy sync

## Structure (of the code)

    mud/
      structure.py       Structure (static content; contains rooms)
      item.py            Item (static content)
      doorway.py         Doorway (static content)
      room.py            Room (static content + live player occupancy)
      world.py           World: loaded structures/rooms, place_player/move_player/
                          remove_player, place_room/move_room_to_structure
      loader.py          JSON -> World
      player.py          Player (dynamic state; identity equality, see game-idea.md)
      telnet_server.py   asyncio telnet server: login (name -> Player), then
                          per-connection command loop
      session.py         Session (holds a Player) / SessionRegistry
      commands/
        command.py         Command interface: match_command, run_command
        runner.py           CommandRunner: dispatches a line to the first match
        look.py, go.py, quit.py   command implementations
      db.py              connection pool, env-based config
      repositories/
        player_repository.py            abstract interface
        postgres_player_repository.py   Postgres implementation
    sql/
      schema.sql        DDL (applied by migrate.py)
    world/
      structures/*.json   example structure data (e.g. old-mill.json)
      rooms/*.json         example room data — each has a "structure_id"
    server.py            entrypoint: loads the world, runs telnet_server

Game logic should depend on `PlayerRepository` (the interface), not on
`PostgresPlayerRepository` or psycopg2 directly — that's what lets the
storage swap later (different DB, a cache, sharding) without touching
anything built on top of it.
