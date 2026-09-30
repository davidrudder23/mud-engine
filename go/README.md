# MUD Engine (Go)

A Go port of the Python engine in `../src`. The two live side by side:
they read the same authored content (`src/world/**/*.json`), the same
schema (`src/sql/schema.sql`), the same `llm.config.json`, and the same
Postgres `players` table. Env vars (`MUD_DB_*`, `MUD_LLM_*`,
`MUD_TELNET_*`, `MUD_MCP_PLAYER_NAME`, `MUD_LOG_LEVEL`) match the Python
version. The design notes in `../README.md` and `../game-idea.md` apply
to both. Command matching differs; see below.

## Command matching

There is one path: `commands.LLMCommandRunner`. It tries each line
locally first, then asks the LLM only if nothing matched:

1. **Exact:** the line starts with one of a command's aliases
   (`go` also answers to `leave`, `exit`, and `travel`).
2. **Fuzzy:** the typed verb is within 1 edit of an alias, then 2
   edits. The limit depends on the alias's length: exact only for 1–3
   letters, 1 edit for 4, and 2 for 5 or more. So `looi` runs `look`,
   but `og` never runs `go`.
3. **LLM:** anything else ("head north please") goes to the model.

Because of this, the server needs LLM config (`MUD_LLM_API_KEY` or
`llm.config.json`) at startup. The Python `MUD_COMMAND_MATCHING=verb`
mode doesn't exist here.

## Running

Run everything from the **project root**, because the default paths
are relative to it:

    go/run.sh                                  # build + run the telnet server
    go build -C go -o bin/ ./cmd/...           # or build every binary into go/bin/
    go/bin/migrate                             # apply src/sql/schema.sql
    go/bin/server                              # telnet on 127.0.0.1:8023
    go/bin/mcp-server                          # MCP over stdio
    go/bin/demo                                # print the loaded world (no DB)

Extra env vars that only the Go binaries read, because Go can't locate
files relative to its own source the way the Python did via `__file__`:

| Var                   | Default               |
| --------------------- | --------------------- |
| `MUD_WORLD_DIR`       | `src/world`           |
| `MUD_SCHEMA_FILE`     | `src/sql/schema.sql`  |
| `MUD_LLM_CONFIG_FILE` | `llm.config.json`     |

## Tests

    cd go && go test ./...          # add -race to check the locking

Nothing needs a database or an API key. `TestCanReachTheConfiguredLLM`
makes one real LLM call, and only when `MUD_LLM_API_KEY` is set.

## Layout

    cmd/server, cmd/mcp-server, cmd/migrate, cmd/demo*   entrypoints
    internal/world        Structure/Room/Item/Doorway/NPC/Player, World
                          bookkeeping, the JSON loader, and the *View
                          snapshots the MCP server returns
    internal/commands     Command interface, one type per verb, BuildAll,
                          LLMCommandRunner (exact, fuzzy, then LLM)
    internal/session      Session + Registry
    internal/repository   PlayerRepository interface + Postgres (pgx)
    internal/db           pgx pool from MUD_DB_* env vars
    internal/llm          Client interface, config, Anthropic client
    internal/npcpatrol    wandering NPCs (one goroutine each)
    internal/telnet       telnet server, ResolvePlayer, BuildRunner
    internal/mcpserver    MCP server (modelcontextprotocol/go-sdk)
    internal/logging      slog setup; logtest captures records in tests
    */*test               shared test fakes: worldtest, repotest, llmtest

## Differences from the Python version

- **Concurrency.** asyncio ran everything on one thread. Here each
  connection and each wandering NPC is a goroutine, so one game lock
  (`telnet.Server.Lock`) wraps every world access. As in the Python,
  where the LLM and DB calls blocked the event loop, a command holds
  the lock for its whole run, including any LLM call.
- **Errors instead of exceptions.** `RunCommand` returns
  `(string, error)`. An error means something beyond what the player
  typed went wrong, such as a failed DB save. The telnet server logs it
  and drops that connection, which is where an uncaught Python
  exception ended up too.
- **The LLM prompt says `string`, not `str`.** It lists parameter types
  as `direction (string, required)`.
- **Structured logs.** Records are `slog` key/value pairs
  (`msg=Login player=alice new_player=true`), each tagged
  `logger=mud.<package>`, rather than formatted strings.
- **Stricter loading.** An item or doorway with no `id`/`direction`/`to`
  is a `LoadError`, where Python raised a bare `KeyError`.
- **Stricter MCP arguments.** A missing or non-string argument gets an
  `isError` tool result.
- **`demo-world-players`** moves between real rooms from the loaded
  world instead of the nonexistent `room-1`/`room-2`.
