# MUD Engine

A text-based multiplayer dungeon engine. Design notes live in
`game-idea.md`.

- Structures/rooms/items/doorways/NPCs are static, authored **content**,
  loaded from `src/world/structures/*.json`, `src/world/rooms/*.json`,
  and `src/world/npcs/*.json` (see `src/mud/loader.py`). Every room
  belongs to exactly one structure (a building, cavern, etc.). Verbs so
  far: `look`/`go`/`quit`/`help`/`pick up`/`drop`/`order`/`talk`.
- Players are dynamic, multi-user **state**, persisted in Postgres (see
  `src/mud/repositories/`). Inventory (what `pick up` adds to) is also
  dynamic, but *not* persisted yet -- it lives only in memory for the
  session, same as room occupancy. NPCs (see `src/mud/npc.py`) have
  their own inventory the same way, per spawned instance (below).
- An `npcs/*.json` id is a *definition* (`World.npcs`), not a specific
  NPC standing anywhere -- each room JSON's own `npcs` list spawns one
  independent instance per entry (`NPC.spawn(room_id)`, which sets that
  instance's `room_id`, mirroring `Player.current_room_id`; a
  definition's own `room_id` stays `None`), so the same id can be
  placed in as many rooms as you like (or more than once in one room),
  e.g. one "shopkeeper" definition backing a distinct shopkeeper -- own
  inventory, own everything -- in every district. Unlike a Player (one
  instance stays in exactly one room for its whole session), an NPC
  definition can back any number of simultaneous room instances.
- NPCs start as "helpers" -- a `hostile` flag exists on the object for
  the eventual enemy/helper split, but nothing reads it yet; there's no
  combat. Behavior beyond `talk` (below) comes from an NPC subclass:
  `WanderingNPC` (see `src/mud/npc.py`) moves itself through a random
  doorway of wherever it's standing every `move_interval` seconds (a
  JSON `"type": "wandering"` definition, e.g. Carrot Ironfoundersson --
  see `src/world/npcs/carrot-ironfoundersson.json`). The moving itself
  is `src/mud/npc_patrol.py`, not the NPC object: one background
  `asyncio` task per WanderingNPC, started by `telnet_server.run()` for
  as long as the server runs, calling `World.move_npc` (mirrors
  `move_player`) on that timer.
- An `LLMClient` interface (see `src/mud/llm/`) wraps calling an LLM,
  with Claude (Anthropic) as the first implementation and the provider
  swappable via config. Two things use it: `LLMCommandRunner` (see
  `src/mud/commands/llm_runner.py`), which matches a typed line to a
  command and extracts its parameters -- an alternative to
  `CommandRunner`'s plain prefix check, selected via
  `MUD_COMMAND_MATCHING` (see below) -- and `talk` (see
  `src/mud/commands/talk.py`), which asks it to improvise one
  in-character line from an NPC. Both need LLM config (below);
  `MUD_COMMAND_MATCHING=llm` requires it at server startup (see below),
  while `talk` only builds its LLMClient the first time someone
  actually uses it, so a server that never sees `talk` typed never
  needs LLM config at all.
- The game is also exposed over MCP (see `src/mcp_server.py` /
  `src/mud/mcp_server.py`) -- one tool per command, for driving a
  single Player from an MCP-capable client (Claude Desktop, Claude
  Code, ...) instead of a human over telnet. Every tool call returns
  the same shape regardless of which command it ran: `{"message":
  ..., "room": ...}` -- that command's own text, plus a fresh,
  structured snapshot of the player's current room (the JSON
  counterpart to what `look` shows). `Command.build_line()` (see
  `command.py`) is what turns a tool call's arguments back into a line
  each command's own `run_command` can parse -- the same mechanism
  `LLMCommandRunner` uses for a model's extracted parameters.

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

### LLM (optional -- only needed for `MUD_COMMAND_MATCHING=llm`)

Config via env vars:

    MUD_LLM_PROVIDER   which LLMClient to build (default "anthropic";
                        it's the only one implemented so far)
    MUD_LLM_API_KEY     required once anything calls get_llm_client() --
                        the selected provider's API key. Left generic on
                        purpose (not e.g. ANTHROPIC_API_KEY) so switching
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
    talk to <npc>    gets one in-character line from an NPC in the room (matches by
                     substring, same as pick up); needs LLM config (see above) -- if
                     that's missing, replies with "<npc> doesn't seem to hear you."
    quit             closes the connection
    help             lists the commands you can use

Anything else gets an "Unknown command" reply -- or, with LLM matching
on (see below), whatever the model couldn't place either. Config via
env vars: `MUD_TELNET_HOST` (default `127.0.0.1`, loopback only — set
to `0.0.0.0` to accept connections from other machines) and
`MUD_TELNET_PORT` (default `8023`).

### Command matching: `MUD_COMMAND_MATCHING`

    verb (default)   CommandRunner: a line must literally start with a
                      command's verb ("go north", not "head north")
    llm               LLMCommandRunner: an LLM decides which command a
                      line invokes and extracts its parameters from
                      free text (e.g. "head north please" -> go north,
                      "grab the sword" -> pick up sword). Needs
                      MUD_LLM_API_KEY (see LLM setup above) -- the
                      server fails fast at startup if it's missing,
                      rather than on the first line someone types.
                      Costs one LLM call per line typed, and a failed
                      call (network, rate limit, ...) falls back to
                      "Unknown command" for that line rather than
                      crashing the connection.

Either way, the underlying commands (and their own argument parsing --
`pick up`'s substring item matching, `go`'s "Go where?" prompt, etc.)
are unchanged; only how a line gets matched to one of them differs.

Logging in twice under the same name is allowed on purpose — you get
two independent sessions of "the same" player, each moving on its own
and both showing up in `Room.players` at once. See `game-idea.md`.

## Run the MCP server

    python3 src/mcp_server.py

Talks stdio JSON-RPC, so it's meant to be spawned by an MCP client, not
run directly in a terminal -- e.g. for Claude Code:

    claude mcp add mud-engine -- python3 /path/to/src/mcp_server.py

or in Claude Desktop's config (`claude_desktop_config.json`):

    { "mcpServers": { "mud-engine": {
        "command": "python3", "args": ["/path/to/src/mcp_server.py"]
    } } }

One player for the whole process -- there's no login flow the way
telnet has one; the player name comes from `MUD_MCP_PLAYER_NAME`
(default `"adventurer"`), resolved exactly like a telnet login (get the
existing player or create one, reset a saved room that no longer
exists). Every command becomes a tool with the same name (spaces
replaced with underscores, e.g. `pick up` -> `pick_up` -- MCP tool
names are identifiers), the same parameters, and the same behavior as
typing it over telnet with `MUD_COMMAND_MATCHING=verb`; only `talk`
additionally needs LLM config (see above) to say anything beyond "...
doesn't seem to hear you."

## Logging

`src/mud/logging_config.py` configures the standard-library `logging`
module: everything logs via `logging.getLogger(__name__)`, which for
this package always nests under a single "mud" logger, given one
handler and a level by `configure_logging()` -- called once, from
`src/server.py`'s `main()`. Level is `MUD_LOG_LEVEL` (default `INFO`):
`DEBUG`, `INFO`, `WARNING`, `ERROR`, or `CRITICAL`.

At `INFO` (the default), `telnet_server.py` logs every connection
opened/closed, every login (new vs. existing player, starting room),
every command line a player sends (session/player/the line itself),
and every logout (with why: `quit`, `disconnect`, or `error`) --
enough to reconstruct a session's whole command history from the log
alone. Also at `INFO`: which command-matching mode was selected at
startup, the world's loaded structure/room/NPC counts, the DB
connection pool being created (host/port/dbname only, never the
password), and -- from `AnthropicLLMClient.complete()` (see
`anthropic_llm_client.py`) -- every actual LLM API call: model,
input/output token counts (from the response's `usage`, so real cost
per call is visible; logged as `None` if a response ever lacks one
rather than crashing), and elapsed time. That last one fires for
*every* LLM call regardless of caller -- `LLMCommandRunner` and `talk`
both end up going through it. `WARNING` adds things worth noticing but
not necessarily acted on: a stale saved room getting reset, a failed
LLM call (from the client itself, and separately from `talk`/
`LLMCommandRunner` about what that failure meant for the command being
handled), a connection-level error. `DEBUG` adds the noisy, per-line
stuff useful mainly while developing: an unmatched command in
`CommandRunner`, and (for `LLMCommandRunner`) every successful match
with the parameters it extracted.

    python3 src/server.py                       # INFO (default)
    MUD_LOG_LEVEL=DEBUG python3 src/server.py    # everything

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
        item.py            Item (static content); get_commands() -> ["pick up"];
                            to_dict()
        doorway.py         Doorway (static content); to_dict()
        npc.py             NPC -- doubles as a definition (World.npcs, one per
                            npcs/*.json id) and a spawned room instance
                            (Room.npcs); spawn(room_id) makes the latter from
                            the former, or from another instance -- own copy
                            of commands/inventory either way, room_id set to
                            wherever it was placed (a definition's is None).
                            eq=False, same reasoning as Player (multiple
                            independent spawns
                            of one definition must stay distinct). get_commands()
                            always includes "talk"; to_dict()
        npc_patrol.py      wander_once() (move one WanderingNPC through a random
                            doorway, or no-op if its room has none) and
                            run_forever() (calls that every npc.move_interval
                            seconds, forever) -- see WanderingNPC above
        room.py            Room (static content + live player occupancy);
                            get_commands() aggregates look/go plus its
                            items'/npcs'/players' own get_commands(); to_dict()
                            (structured counterpart to format(), for the MCP
                            server -- nests every item's/npc's/player's/
                            doorway's own to_dict())
        world.py           World: loaded structures/rooms/npcs, place_player/
                            move_player/remove_player/move_npc, place_room/
                            move_room_to_structure, get_room/get_structure/get_npc
        loader.py          JSON -> World
        player.py          Player (dynamic state; identity equality, see
                            game-idea.md); to_dict()
        telnet_server.py   asyncio telnet server: login (name -> Player), then
                            per-connection command loop; logs connection open/
                            close, login, every command line, and logout (see
                            Logging above); run() also starts/cancels one
                            npc_patrol task per WanderingNPC the world has
        logging_config.py  configure_logging(): one handler on the "mud"
                            logger, level from MUD_LOG_LEVEL
        session.py         Session (holds a Player) / SessionRegistry
        mcp_server.py      build_server(): MCPServer with one tool per
                            _build_commands() command (telnet_server.py's exact
                            command list); every tool returns {"message", "room"}
                            (room via Room.to_dict())
        commands/
          command.py         Command interface: name, match_command, match_room,
                              describe, parameters (declared inputs -- name/type/
                              required), run_command, build_line (parameter values
                              -> a line run_command can parse -- used by both
                              LLMCommandRunner and the MCP server)
          runner.py           CommandRunner: matches a line by verb prefix
                              (matches_verb) and dispatches to the first match
          llm_runner.py        LLMCommandRunner: same shape as CommandRunner,
                              but asks an LLMClient which command (built from
                              each command's name/describe/parameters) a line
                              invokes and what its parameter values are, then
                              reconstructs a normalized line ("go north") for
                              that command's own run_command to parse as usual
          look.py, go.py, quit.py, help.py, pick_up.py, drop.py, order.py,
          talk.py             command implementations. talk.py builds its
                              LLMClient lazily (on first use, not at server
                              startup) via a `get_llm_client` it takes as a
                              constructor arg, defaulting to
                              mud.llm.config.get_llm_client
        db.py              connection pool, env-based config
        repositories/
          player_repository.py            abstract interface
          postgres_player_repository.py   Postgres implementation
        llm/
          llm_client.py          abstract interface: complete(prompt, system=...)
          config.py               MUD_LLM_PROVIDER-based factory, env-based config
          anthropic_llm_client.py Claude implementation, via the anthropic SDK;
                                   logs every call (model, input/output tokens
                                   from the response's usage, elapsed time)
      sql/
        schema.sql        DDL (applied by migrate.py)
      world/
        structures/*.json   3 structures: unseen-university, pseudopolis-yard, mended-drum
        rooms/*.json         15 rooms (5 each), each with a "structure_id" —
                              a rough, original-text homage to Ankh-Morpork
        npcs/*.json          1 so far: "tavern-keeper" (Grubb), placed in
                              drum-taproom
      server.py            entrypoint: loads the world, runs telnet_server
      mcp_server.py        entrypoint: loads the world, runs the MCP server
                            (mud/mcp_server.py) over stdio
    test/
      fakes.py             FakePlayerRepository, fake asyncio streams,
                              FakeAnthropicClient, FakeLLMClient
      helpers.py           shared two-room World builder
      test_loader.py       JSON -> World, incl. malformed/inconsistent data,
                              npcs/*.json + room "npcs" references, each
                              reference spawning its own independent instance,
                              and npc "type" dispatch (plain vs. wandering)
      test_world.py        room/player/npc placement & movement bookkeeping
      test_npc.py           NPC defaults, format(), get_commands(), equality
                              (eq=False), spawn(), WanderingNPC
      test_npc_patrol.py    wander_once() and run_forever()'s loop/cancellation
      test_room.py         Room/Item/Player/NPC get_commands(), Room.format()
      test_commands.py     look/go/quit/pick up/drop/order/help/talk +
                              matches_verb + CommandRunner
      test_llm_runner.py    LLMCommandRunner, via a scripted FakeLLMClient,
                              incl. its warning/info/debug log records
      test_telnet_server.py  end-to-end: login/commands/disconnect over fake
                              streams, no socket or DB required; also covers
                              MUD_COMMAND_MATCHING's verb/llm selection and
                              the login/command/logout log records
      test_logging_config.py configure_logging(): MUD_LOG_LEVEL parsing
      test_mcp_server.py     one tool per command, each tool's schema (from
                              Command.parameters()), and the {"message", "room"}
                              shape every tool call returns -- drives the real
                              MCPServer's list_tools()/call_tool(), no fakes

Game logic should depend on `PlayerRepository` (the interface), not on
`PostgresPlayerRepository` or psycopg2 directly — that's what lets the
storage swap later (different DB, a cache, sharding) without touching
anything built on top of it.
