# MUD Engine

A text-based multiplayer dungeon engine. Design notes live in
`game-idea.md`.

- Structures/rooms/items/doorways are static, authored **content**,
  loaded from `src/world/structures/*.json` and `src/world/rooms/*.json`
  (see `src/mud/loader.py`). Every room belongs to exactly one structure
  (a building, cavern, etc.). Verbs so far: `look`/`go`/`quit`/`help`/
  `pick up`/`drop`/`order`.
- Players are dynamic, multi-user **state**, persisted in Postgres (see
  `src/mud/repositories/`). Inventory (what `pick up` adds to) is also
  dynamic, but *not* persisted yet -- it lives only in memory for the
  session, same as room occupancy.
- An `LLMClient` interface (see `src/mud/llm/`) wraps calling an LLM,
  with Claude (Anthropic) as the first implementation and the provider
  swappable via config. Not wired into the command runner yet -- the
  eventual goal is matching a typed line to a command and its
  parameters via an LLM, but for now it's just the client/config,
  callable on its own.

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

    python3 src/migrate.py

Connection settings default to a local Unix-socket connection as your
OS user against a database named `mud`. Override with env vars if
needed: `MUD_DB_HOST`, `MUD_DB_PORT`, `MUD_DB_NAME`, `MUD_DB_USER`,
`MUD_DB_PASSWORD`. (Only `MUD_DB_HOST` and `MUD_DB_PASSWORD` need
setting for a typical remote Postgres — the rest have sane defaults.)

### LLM (optional, not wired up yet)

Only needed once something actually calls `get_llm_client()` — nothing
in the running game does yet. Config via env vars:

    MUD_LLM_PROVIDER   which LLMClient to build (default "anthropic";
                        it's the only one implemented so far)
    MUD_LLM_API_KEY     required once you do call it — the selected
                        provider's API key. Left generic on purpose
                        (not e.g. ANTHROPIC_API_KEY) so switching
                        MUD_LLM_PROVIDER later doesn't mean renaming
                        env vars too.
    MUD_LLM_MODEL       optional; defaults to "claude-sonnet-5" for the
                        anthropic provider (see anthropic_llm_client.py)

## Run the server

    python3 src/server.py

Then connect with a telnet client:

    telnet localhost 8023

You'll be asked for a name — type anything; there's no password, and
that name either becomes an existing player or creates a new one. Try:

    look             re-describes the room you're in, plus a "you can try: ..." hint
    go <dir>         moves through a doorway (exact, case-sensitive match, e.g. "go north")
    pick up <item>   moves an item from the room into your inventory (matches by
                     case-insensitive substring against the item's id or name)
    drop <item>      moves an item from your inventory into the room you're
                     currently standing in (reverse of pick up; same matching)
    order <drink>    flavor text only -- no inventory or cost, just echoes back what you asked for
    quit             closes the connection
    help             lists the commands you can use

Anything else gets an "Unknown command" reply. Config via env vars:
`MUD_TELNET_HOST` (default `127.0.0.1`, loopback only — set to
`0.0.0.0` to accept connections from other machines) and
`MUD_TELNET_PORT` (default `8023`).

Logging in twice under the same name is allowed on purpose — you get
two independent sessions of "the same" player, each moving on its own
and both showing up in `Room.players` at once. See `game-idea.md`.

## Try it

    python3 src/demo.py               # loads world/{structures,rooms}/*.json — no DB needed
    python3 src/demo_player.py        # creates/moves/looks up a player in Postgres
    python3 src/demo_world_players.py # room <-> player occupancy sync

## Tests

No DB needed — Postgres-backed code is exercised through an in-memory
fake repository (`test/fakes.py`), not `PostgresPlayerRepository`.

    python3 -m unittest discover -v

One test, `test/test_llm_integration.py`, makes a real call to the
configured LLM to confirm actual connectivity (network, API key, model
name all correct together) — unlike every other test here, which is
offline. It auto-skips if no API key is configured; fill one in (see
LLM setup above) to have it actually run, and note it'll spend a small
number of tokens each time you run the suite.

## Structure (of the code)

    src/
      mud/
        structure.py       Structure (static content; contains rooms)
        item.py            Item (static content); get_commands() -> ["pick up"]
        doorway.py         Doorway (static content)
        room.py            Room (static content + live player occupancy);
                            get_commands() aggregates look/go plus its
                            items'/players' own get_commands()
        world.py           World: loaded structures/rooms, place_player/move_player/
                            remove_player, place_room/move_room_to_structure
        loader.py          JSON -> World
        player.py          Player (dynamic state; identity equality, see game-idea.md)
        telnet_server.py   asyncio telnet server: login (name -> Player), then
                            per-connection command loop
        session.py         Session (holds a Player) / SessionRegistry
        commands/
          command.py         Command interface: match_command, match_room, describe,
                              parameters (declared inputs -- name/type/required, unused
                              by run_command yet -- see command.py's docstring), run_command
          runner.py           CommandRunner: dispatches a line to the first match
          look.py, go.py, quit.py, help.py, pick_up.py, drop.py, order.py
                              command implementations
        db.py              connection pool, env-based config
        repositories/
          player_repository.py            abstract interface
          postgres_player_repository.py   Postgres implementation
        llm/
          llm_client.py          abstract interface: complete(prompt, system=...)
          config.py               MUD_LLM_PROVIDER-based factory, env-based config
          anthropic_llm_client.py Claude implementation, via the anthropic SDK
      sql/
        schema.sql        DDL (applied by migrate.py)
      world/
        structures/*.json   3 structures: unseen-university, pseudopolis-yard, mended-drum
        rooms/*.json         15 rooms (5 each), each with a "structure_id" —
                              a rough, original-text homage to Ankh-Morpork
      server.py            entrypoint: loads the world, runs telnet_server
    test/
      fakes.py             FakePlayerRepository + fake asyncio streams
      helpers.py           shared two-room World builder
      test_loader.py       JSON -> World, incl. malformed/inconsistent data
      test_world.py        room/player placement & movement bookkeeping
      test_room.py         Room/Item/Player get_commands()
      test_commands.py     look/go/quit + matches_verb + CommandRunner
      test_telnet_server.py  end-to-end: login/commands/disconnect over
                              fake streams, no socket or DB required

Game logic should depend on `PlayerRepository` (the interface), not on
`PostgresPlayerRepository` or psycopg2 directly — that's what lets the
storage swap later (different DB, a cache, sharding) without touching
anything built on top of it.
