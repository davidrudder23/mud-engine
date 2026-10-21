# MUD Engine

A text-based multiplayer dungeon engine, written in Go. Design
notes live in `game-idea.md`.

- Structures/rooms/items/doorways/NPCs are static, authored **content**,
  loaded from `world/structures/*.json`, `world/rooms/*.json`,
  and `world/npcs/*.json` (see `internal/world/loader.go`). Every
  room belongs to exactly one structure (a building, cavern, etc.).
  Verbs so far: `look`/`go`/`quit`/`help`/`pick up`/`drop`/`inventory`/
  `stats`/`attack`/`throw`/`browse`/`buy`/`sell`/`order`/`talk`/`say`/
  `teleport`.
- Players are dynamic, multi-user **state**, persisted in Postgres (see
  `internal/repository/`). Money is too (see Shops below). Inventory
  (what `pick up` adds to) is also dynamic, but *not* persisted yet --
  it lives only in memory for the session, same as room occupancy. NPCs
  have their own inventory the same way, per spawned instance (below).
- An `npcs/*.json` id is a *definition* (`World.NPCs`), not a specific
  NPC standing anywhere -- each room JSON's own `npcs` list spawns one
  independent instance per entry (`NPC.Spawn(roomID)`), so the same id
  can be placed in as many rooms as you like (or more than once in one
  room), e.g. one "shopkeeper" definition backing a distinct
  shopkeeper -- own inventory, own everything -- in every district.
- An NPC is either an **enemy** (`"enemy": true`, with stats, and
  can be fought -- see Combat below) or a helper, which can't be. A
  `"type": "wandering"` NPC (e.g. Carrot Ironfoundersson, see
  `world/npcs/carrot-ironfoundersson.json`) moves itself through a
  random doorway every `move_interval` seconds; the moving is
  `internal/npcpatrol`, one goroutine per wandering NPC for as long
  as the server runs.
- NPCs and items can be **scripted in Lua** (`"script": "rat.lua"`,
  a file in `world/scripts/`), and a script can be loaded, reloaded,
  and unloaded while the server runs -- see Scripting below.
- An `llm.Client` interface (`internal/llm/`) wraps calling an LLM,
  with Claude (Anthropic) as the first implementation and the provider
  swappable via config. Two things use it: command matching (below)
  and conversations with NPCs (see Talking to NPCs below).
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

`migrate` is safe to re-run, and upgrades an existing database in
place: run it again whenever `sql/schema.sql` changes.

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
    teleport <room>  moves you straight to any room, by its id (case-insensitive; a
                     unique part of the id is enough, e.g. "teleport library")
    pick up <item>   moves an item from the room into your inventory (case-insensitive
                     substring match against the item's id or name)
    drop <item>      moves an item from your inventory into the current room
    inventory        lists what you're carrying
    stats            shows your hit points, stamina, stats, weapon, and money
    attack <npc>     fights an enemy in the room (see Combat below)
    throw <item> <dir>  throws a spear that way, e.g. "throw spear east" (see Combat below)
    browse           lists what the shop you're standing in sells, with prices (see
                     Shops below); also typed as wares
    buy <item>       buys something from the shop; also typed as purchase
    sell <item>      sells something you're carrying to the shop
    order <drink>    flavor text only -- just echoes back what you asked for
    talk to <npc>    starts a conversation with an NPC in the room (see
                     Talking to NPCs below)
    say <text>       everyone else in the room sees "<you> says: <text>";
                     whoever you're talking to answers
    quit             closes the connection
    help             lists the commands you can use
    reload <script>  loads the latest version of a Lua script (wizzards only)
    unload <script>  stops a Lua script running (wizzards only)

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
   2 edits for 4 or more. So `looi` runs `look`, but `og` never runs
   `go`.
3. **LLM:** anything else ("grab the sword") goes to the model,
   which picks a command and extracts its parameters. A failed or
   unparseable reply is "Unknown command".

Either way, each command's own argument parsing (`pick up`'s substring
matching, `go`'s "Go where?" prompt, ...) is what finally runs.

## Combat

Players and enemy NPCs fight with the same rules (`internal/combat`):
both have **strength, dexterity, luck, hit points, and stamina**.
`attack <npc>` is one exchange: you attack, then the enemy strikes
back if it's still standing.

| Stat | Effect |
|---|---|
| Dexterity | chance to hit: 70% when evenly matched, ±5% per point of difference, clamped to 10–95% |
| Strength | damage = wielded weapon's damage + half your strength (at least 1) |
| Luck | percent chance a hit is critical (double damage), up to 50% |
| Stamina | each attack costs 2; with less you can't attack. Everyone regains 1 every 3 seconds |
| Hit points | damage comes off these; at 0 or below you're dead |

- **Weapons** are items with a `"weapon": {"damage": N}`. You fight
  with the best one you're carrying; with none, bare hands.
- **Spears** are weapons with `"type": "spear"`, which can also be
  thrown: `throw spear east`. A thrown spear looks for an enemy in
  your own room first, then flies room to room in that direction
  until it finds one or there's no doorway that way. At the first
  enemy it's one blow with the spear, which can miss; either way it
  stops there, and the enemy doesn't strike back. A throw costs the
  same stamina as an attack, and the spear ends up on the floor where
  its flight ended. There's one in the Watch's armory.
- **An enemy that dies** drops everything it carried into the room
  (so `pick up` loots it) and is gone. No respawn yet.
- **A player who dies** comes to in the starting room, fully healed,
  still carrying everything.
- Only enemies can be attacked. Enemies only fight back; none start
  fights yet. No player-vs-player.
- Player stats are saved in Postgres after every exchange. New players
  start at strength/dexterity/luck 5, 20 hit points, 10 stamina, and
  AM$10 (see Shops below).
  Stamina only regenerates while you're logged in.

An enemy's stats (all five required) and weapons are authored in its
NPC file, e.g. `world/npcs/drunken-brawler.json`:

    "enemy": true,
    "stats": { "strength": 7, "hit_points": 16, "stamina": 8, "luck": 2, "dexterity": 3 },
    "inventory": [ { "id": "broken-bottle", "name": "a broken bottle", "weapon": { "damage": 3 } } ]

The Mended Drum has two so far: a rat in the cellar and a brawler
upstairs. The Watch's holding cells have a third, a swamp dragon.

## Shops

A room with a `"shop"` is somewhere to buy and sell; the first is
Burleigh & Stronginthearm, the weapon shop south of the Watch's front
desk (`world/rooms/bs-showroom.json`).

- **Money** is Ankh-Morpork dollars (`AM$`), whole ones only. A new
  player starts with AM$10. It's saved in Postgres after every sale, so
  it survives a logout even though what you bought doesn't.
- **Value:** any item can have a `"value"`. A shop charges that for it
  and pays half that (rounded down, at least AM$1) for one. An item
  with no value can't be sold.
- **Stock** never runs out: `buy` hands over a copy. What a shop buys
  from you isn't put on sale; it's gone.
- `browse`, `buy`, and `sell` only show up in `help` where there's a
  shop, and say "There's no shop here." anywhere else.

A shop is authored on its room; every stock item needs a value, and
none can have a script:

    "shop": { "stock": [
      { "id": "dagger", "name": "a serviceable dagger", "weapon": { "damage": 2 }, "value": 3 }
    ] }

The shopkeeper is an ordinary NPC, there to `talk` to -- the shop
belongs to the room and trades whether or not anyone's behind the
counter.

## Talking to NPCs

Any NPC can hold a conversation, played by the LLM. `talk to <npc>`
starts one: the NPC greets you, and from then on answers whatever you
`say`. Everyone else in the room hears both sides. Try it on Snorri in
the weapon shop:

    > talk to snorri
    Snorri Stronginthearm says: Good morning, sir. ...
    (You're talking with Snorri Stronginthearm. Use say <text> to answer.)
    > say What would you recommend for a beginner?
    You say: What would you recommend for a beginner?
    Snorri Stronginthearm says: ...

- **What the NPC knows:** its name, `description`, and `persona`; the
  room and building it's in, and who else is there; what it's
  carrying; and, in a shop, the stock with its prices. Nothing else, so
  it shouldn't make up items or prices.
- **Talk only:** an NPC can't hand anything over or change the game.
  When you want something done, it should point you to the command
  (`buy`, say).
- **Memory:** an NPC remembers the last 20 lines of your conversation
  (yours and its own), only for as long as the conversation lasts. It
  ends when you `talk` to someone else, or when you `say` something
  with either of you gone from the room it started in. Nothing is
  saved to the database.
- **Persona:** an NPC file can add a `"persona"`: private notes for the
  LLM on how the NPC speaks, what it knows, and what it wants. Players
  never see it directly. Without one, the NPC is played from its
  `description` alone:

      "persona": "You speak formally, call customers 'sir' or 'madam', and never haggle."

- If the LLM can't be reached, the NPC "doesn't seem to hear you" and
  the conversation carries on as if you hadn't spoken.
- The game waits while the NPC replies (usually a second or two), the
  same as for any command the LLM has to match.

## Scripting

NPCs and items can be given behaviour in Lua, without recompiling or
restarting: edit the script, type `reload <script>` in the game, and
everything already using it behaves the new way, keeping its state.
`lua_examples.md` is the guide for the wizzards writing scripts;
`lua_scripting.md` is the design.

- **Attaching a script:** add `"script": "swamp-dragon.lua"` to an NPC
  file or to an inline item, naming a file in `world/scripts/`. The
  world ships three: `cellar-rat.lua`, `swamp-dragon.lua` (in the
  Watch's holding cells), and `muttering-sword.lua` (in the armory).
- **What a script is:** a class extending `Monster` or `Item` that
  overrides hooks -- `on_enter`, `on_say`, `on_attacked`, `on_death`,
  `on_tick` for a monster; `on_pickup`, `on_drop`, `on_hit` for an
  item. Anything it doesn't override behaves as it would unscripted.
- **Who can reload:** the players named in `MUD_WIZZARDS`
  (comma-separated, case-insensitive; empty by default, so nobody).
  There's no real auth, so this is only as strong as who can reach
  the telnet port.
- **What a reload checks:** the new version is run against simulated
  traffic first and rejected -- with the reason shown to the wizzard,
  and the old version left running -- if it doesn't compile, a hook
  fails, or it allocates too much memory or keeps holding more of it.
- **At run time:** scripts see only Lua's `string`, `table`, and
  `math` libraries plus the game API; every hook runs under a 250ms
  deadline; and a script whose hooks fail 5 times in a row is disabled
  until it's reloaded.

Scripts are hot-reloadable, the JSON isn't: giving an NPC or item a
script for the first time (or adding a new scripted NPC) still takes a
restart, since the world JSON is only read at startup.

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

`internal/scripting` includes a test that runs every script the world
uses through the same admission check `reload` does, so a script that
wouldn't load fails the build.

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
      world               Structure/Room/Item/Doorway/NPC/Player/Shop, World
                          bookkeeping, the JSON loader, and the *View
                          snapshots the MCP server returns
      combat              combat rules (Attack, hit/damage/crit) and
                          stamina regeneration
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
      */*test             shared test fakes: worldtest, repotest, llmtest,
                          combattest

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
