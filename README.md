# MUD Engine

A text-based multiplayer dungeon engine, written in Go. Design
notes live in `game-idea.md`.

- Structures/rooms/items/doorways/NPCs are static, authored **content**,
  loaded from `world/structures/*.json`, `world/rooms/*.json`,
  and `world/npcs/*.json` (see `internal/world/loader.go`). Every
  room belongs to exactly one structure (a building, cavern, etc.).
  Verbs so far: `look`/`go`/`quit`/`help`/`pick up`/`drop`/`order`/`talk`.
- Players are dynamic, multi-user **state**, persisted in Postgres (see
  `internal/repository/`). Inventory (what `pick up` adds to) is
  also dynamic, but *not* persisted yet -- it lives only in memory for
  the session, same as room occupancy. NPCs have their own inventory
  the same way, per spawned instance (below).
- An `npcs/*.json` id is a *definition* (`World.NPCs`), not a specific
  NPC standing anywhere -- each room JSON's own `npcs` list spawns one
  independent instance per entry (`NPC.Spawn(roomID)`), so the same id
  can be placed in as many rooms as you like (or more than once in one
  room), e.g. one "shopkeeper" definition backing a distinct
  shopkeeper -- own inventory, own everything -- in every district.
- NPCs start as "helpers" -- a `Hostile` flag exists for the eventual
  enemy/helper split, but nothing reads it yet; there's no combat. A
  `"type": "wandering"` NPC (e.g. Carrot Ironfoundersson, see
  `world/npcs/carrot-ironfoundersson.json`) moves itself through a
  random doorway every `move_interval` seconds; the moving is
  `internal/npcpatrol`, one goroutine per wandering NPC for as long
  as the server runs.
- An `llm.Client` interface (`internal/llm/`) wraps calling an LLM,
  with Claude (Anthropic) as the first implementation and the provider
  swappable via config. Two things use it: command matching (below)
  and `talk`, which asks it to improvise one in-character line from an
  NPC.
- The game is also exposed over MCP (`internal/mcpserver/`) -- one
  tool per command, for driving a single Player from an MCP-capable
  client (Claude Desktop, Claude Code, ...) instead of a human over
  telnet. Every tool call returns `{"message": ..., "room": ...}`: that
  command's own text, plus a fresh, structured snapshot of the player's
  current room.

## Setup

Requires Go (1.25+) and PostgreSQL. On this machine PostgreSQL is
already installed and a `mud` database exists, owned by the local OS
user, using peer auth (no password). To set this up elsewhere:

    sudo apt-get install postgresql postgresql-contrib
    sudo systemctl start postgresql
    sudo -u postgres createuser --createdb "$(whoami)"
    sudo -u postgres createdb -O "$(whoami)" mud

Build everything and create the schema (all commands here run from the
project root, because the default file paths are relative to it):

    go build -o bin/ ./cmd/...    # binaries land in bin/
    bin/migrate                   # applies sql/schema.sql

Connection settings default to a local Unix-socket connection as your
OS user against a database named `mud`. Override with `MUD_DB_HOST`,
`MUD_DB_PORT`, `MUD_DB_NAME`, `MUD_DB_USER`, `MUD_DB_PASSWORD`.

### LLM (required)

Every line a player types goes through the LLM command runner, so the
server needs LLM config at startup. Set it via env vars, or the same
keys in `llm.config.json` (gitignored; env vars win):

    MUD_LLM_PROVIDER   default "anthropic" (the only one implemented)
    MUD_LLM_API_KEY    the provider's API key ("api_key" in the file)
    MUD_LLM_MODEL      optional; default "claude-sonnet-5"

## Run the server

    ./run.sh        # builds bin/server, then runs it

Then connect with a telnet client:

    telnet localhost 8023

You'll be asked for a name -- type anything; there's no password, and
that name either becomes an existing player or creates a new one. Try:

    look             re-describes the room you're in, plus a "you can try: ..." hint
    go <dir>         moves through a doorway (exact, case-sensitive, e.g. "go north");
                     also typed as leave, exit, or travel
    pick up <item>   moves an item from the room into your inventory (case-insensitive
                     substring match against the item's id or name)
    drop <item>      moves an item from your inventory into the current room
    order <drink>    flavor text only -- just echoes back what you asked for
    talk to <npc>    gets one in-character line from an NPC in the room
    quit             closes the connection
    help             lists the commands you can use

`MUD_TELNET_HOST` (default `127.0.0.1`; `0.0.0.0` to accept other
machines) and `MUD_TELNET_PORT` (default `8023`) configure the
listener.

Logging in twice under the same name is allowed on purpose -- you get
two independent sessions of "the same" player, each moving on its own.

### Command matching

`commands.LLMCommandRunner` tries each line locally first, and asks the
LLM only if nothing matched:

1. **Exact:** the line starts with one of a command's aliases.
2. **Fuzzy:** the typed verb is within 1 edit of an alias, then 2. The
   limit depends on the alias's length: exact only for 1–3 letters,
   1 edit for 4, 2 for 5 or more. So `looi` runs `look`, but `og`
   never runs `go`.
3. **LLM:** anything else ("head north please") goes to the model,
   which picks a command and extracts its parameters. A failed or
   unparseable reply is "Unknown command".

Either way, each command's own argument parsing (`pick up`'s substring
matching, `go`'s "Go where?" prompt, ...) is what finally runs.

## Run the MCP server

    bin/mcp-server

It talks stdio JSON-RPC, so an MCP client spawns it -- e.g. for Claude
Code (run from the project root, so the default paths resolve):

    claude mcp add mud-engine -- /path/to/mud-engine/bin/mcp-server

One player per process: the name comes from `MUD_MCP_PLAYER_NAME`
(default `"adventurer"`), resolved exactly like a telnet login. Every
command becomes a tool with the same name (spaces become underscores:
`pick_up`) and the same parameters.

## Logging

Logs go to stderr as `log/slog` key/value text, each record tagged
`logger=mud.<package>`. Level is `MUD_LOG_LEVEL` (default `INFO`):
`DEBUG`, `INFO`, `WARNING`, `ERROR`, or `CRITICAL`.

At `INFO`: connections opened/closed, logins (new vs. existing player),
every command line a player sends, logouts (with reason: `quit`,
`disconnect`, or `error`), world/DB startup details, and every LLM call
(model, token counts, elapsed time). `WARNING` adds stale saved rooms
being reset, failed LLM calls, and connection errors. `DEBUG` adds
fuzzy matches, LLM matches with their extracted parameters, and the
built LLM system prompt.

## Try it

    bin/demo                  # prints the loaded world -- no DB needed
    bin/demo-player           # creates/moves/looks up a player in Postgres
    bin/demo-world-players    # room <-> player occupancy staying in sync

## Tests

    go test ./...                # add -race to check the locking

No database or API key needed: Postgres is replaced by an in-memory
fake repository and the LLM by a scripted fake. One test,
`TestCanReachTheConfiguredLLM`, makes a real call to confirm
connectivity; it skips unless `MUD_LLM_API_KEY` is set.

## Layout

    world/                authored content (structures, rooms, npcs JSON)
    sql/schema.sql        Postgres schema (applied by migrate)
    cmd/                  server, mcp-server, migrate, demo* entrypoints
    bin/                  build output (gitignored)
    internal/
      world               Structure/Room/Item/Doorway/NPC/Player, World
                          bookkeeping, the JSON loader, and the *View
                          snapshots the MCP server returns
      commands            Command interface, one type per verb, BuildAll,
                          LLMCommandRunner (exact, fuzzy, then LLM)
      session             Session + Registry
      repository          PlayerRepository interface + Postgres (pgx)
      db                  pgx pool from MUD_DB_* env vars
      llm                 Client interface, config, Anthropic client
      npcpatrol           wandering NPCs (one goroutine each)
      telnet              telnet server, ResolvePlayer, BuildRunner
      mcpserver           MCP server (modelcontextprotocol/go-sdk)
      logging             slog setup; logtest captures records in tests
      config              MUD_WORLD_DIR / MUD_SCHEMA_FILE defaults
      */*test             shared test fakes: worldtest, repotest, llmtest

Each connection and each wandering NPC runs in its own goroutine; one
game lock (`telnet.Server.Lock`) wraps every world access, so the game
logic itself stays single-threaded. A command holds that lock for its
whole run, including any LLM call or DB save.

`MUD_WORLD_DIR` (default `world`), `MUD_SCHEMA_FILE` (default
`sql/schema.sql`), and `MUD_LLM_CONFIG_FILE` (default
`llm.config.json`) override the file locations.

Game logic depends on `repository.PlayerRepository` (the interface),
not on the Postgres implementation or pgx directly -- that's what lets
the storage swap later without touching anything built on top of it.
