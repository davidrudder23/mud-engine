# MUD Engine

A text-based multiplayer dungeon engine. Design notes live in
`game-idea.md`.

- Rooms/items/doorways are static, authored **content**, loaded from
  `world/rooms/*.json` (see `mud/loader.py`). No verbs/actions yet.
- Players are dynamic, multi-user **state**, persisted in Postgres (see
  `mud/repositories/`).

## Setup

Requires PostgreSQL and the `psycopg2` driver. On this machine both are
already installed and a `mud` database exists, owned by the local OS
user, using peer auth (no password). To set this up elsewhere:

    sudo apt-get install postgresql postgresql-contrib python3-psycopg2
    sudo systemctl start postgresql
    sudo -u postgres createuser --createdb "$(whoami)"
    sudo -u postgres createdb -O "$(whoami)" mud

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

You'll be dropped into a random room. Try:

    look        re-describes the room you're in
    go <dir>    stub for now — always replies "lorem ipsum"

Anything else gets an "Unknown command" reply. Config via env vars:
`MUD_TELNET_HOST` (default `127.0.0.1`, loopback only — set to
`0.0.0.0` to accept connections from other machines) and
`MUD_TELNET_PORT` (default `8023`).

## Try it

    python3 demo.py               # loads world/rooms/*.json — no DB needed
    python3 demo_player.py        # creates/moves/looks up a player in Postgres
    python3 demo_world_players.py # room <-> player occupancy sync

## Structure

    mud/
      item.py            Item (static content)
      doorway.py         Doorway (static content)
      room.py            Room (static content + live player occupancy)
      world.py           World: loaded rooms + place_player/move_player
      loader.py          JSON -> World
      player.py          Player (dynamic state)
      telnet_server.py   asyncio telnet server (per-connection handler)
      session.py         Session / SessionRegistry (per-connection state)
      commands/
        command.py         Command interface: match_command, run_command
        runner.py           CommandRunner: dispatches a line to the first match
        look.py, go.py       command implementations
      db.py              connection pool, env-based config
      repositories/
        player_repository.py            abstract interface
        postgres_player_repository.py   Postgres implementation
    sql/
      schema.sql        DDL (applied by migrate.py)
    world/rooms/*.json   example world data
    server.py            entrypoint: loads the world, runs telnet_server

Game logic should depend on `PlayerRepository` (the interface), not on
`PostgresPlayerRepository` or psycopg2 directly — that's what lets the
storage swap later (different DB, a cache, sharding) without touching
anything built on top of it.
