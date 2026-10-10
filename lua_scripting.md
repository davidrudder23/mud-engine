# Lua Scripting — Plan

Status: planned, nothing built yet. This is the design agreed before
starting work on the `lua_dynamic_loading` branch.

## Goal

Let vetted wizzards add code to the running MUD: write a Lua script
that gives an NPC (and later a room, an item, or a verb) custom
behaviour, then load, reload, or unload it without recompiling or
restarting the server. The feel to aim for is the old LPC one of
writing `extend Monster` and typing `update`.

## Decisions

- **Interpreter: [gopher-lua](https://github.com/yuin/gopher-lua).**
  A Lua 5.1 VM in pure Go (no cgo), embedded in the server. It sits
  behind a small Go interface of our own (`Load`, `CallHook`,
  `Unload`) so nothing else in the engine imports it directly and it
  can be swapped later.
- **Authors: vetted wizzards only.** The safety goal is catching
  accidents, not resisting attacks. If scripting is ever opened to
  ordinary players, this needs revisiting (see Safety).
- **Data and code stay separate.** `world/npcs/*.json` remain the
  definitions and gain an optional `"script"` field naming a Lua
  file. An NPC with no script behaves exactly as it does today.
- **Class model.** The existing definition/instance split is already
  class/object: a script is loaded once per definition as the class,
  and each `NPC.Spawn` creates an instance table whose metatable
  points at it. Wizzards write `Monster:extend()`. Today's hardcoded
  variants (`"enemy": true`, `"type": "wandering"`) become the base
  classes (`Monster`, `Wanderer`) in a small Lua standard library
  shipped with the engine.
- **Threading.** A Lua state isn't goroutine-safe. There is one state
  for the world, touched only while holding the existing game lock
  (the one telnet, MCP, `npcpatrol`, and combat regen already share).
- **Hooks.** Go calls into Lua at events: `on_enter`, `on_say`,
  `on_attacked`, `on_death`, `on_tick`. A missing hook falls back to
  the current Go behaviour.
- **Narrow API.** Scripts get handles, not Go pointers: `self:say()`,
  `self:emote()`, `self:move()`, `room:players()`, `player:send()`.
  Everything that changes the world routes through `World.MovePlayer`
  / `MoveNPC` / `KillNPC`, so a script can't break the occupancy
  invariants described in `game-idea.md`.
- **Scripted verbs (later).** `NPC.Commands` and `Room.Commands`
  already list verbs that mostly have no implementation. A
  `ScriptCommand` implementing the `Command` interface, with its
  name, description, and parameters declared in Lua, would get LLM
  matching for free.

What a script should look like:

```lua
local Rat = Monster:extend()

function Rat:on_enter(player)
  self:emote("bares its teeth at " .. player.name .. ".")
end

function Rat:on_death(killer)
  self.room:spawn("cellar-rat")   -- there's always another rat
end

return Rat
```

## Load, reload, unload

- **Trigger.** An in-game `reload <id>` command, wizzards only. File
  watching can come later.
- **Reload** swaps the methods inside the existing class table rather
  than replacing the table, so every live instance picks up the new
  behaviour immediately and keeps its own state.
- **Unload** drops the class; its instances fall back to the Go
  defaults.
- **A bad script never replaces a good one.** If the new version
  fails to compile, or fails the admission harness below, the old
  version keeps running and the wizzard gets the error.
- Script state is in-memory only, the same as inventory and room
  occupancy today.

## Safety

- **Sandbox.** Only the `base`, `table`, `string`, and `math`
  libraries are opened. No `io`, `os`, `debug`, `load`, or the real
  `require`.
- **CPU.** Every hook call runs under a context deadline
  (`L.SetContext`). Hooks run while holding the game lock, so without
  this an infinite loop would freeze the whole MUD.
- **Memory: an admission harness.** gopher-lua has no memory limit,
  so memory is checked before a script goes live rather than while it
  runs. The harness loads the script into a scratch Lua state with a
  throwaway world, fires simulated traffic at its hooks, and measures
  with `runtime.ReadMemStats`:
  - `TotalAlloc` delta -- everything allocated during the burst, an
    upper bound on peak usage. Catches `s = s .. s` style blowups.
  - `HeapAlloc` delta after `runtime.GC()` -- what is still held.
    Run at two event counts (say 1,000 and 2,000) and fail if
    retained memory grows with the count, which is a leak.

  It runs in two places: `go test`, over every script under `world/`,
  and inside `reload`, before the new version is swapped in.
- **Errors.** Every hook runs in a protected call. Failures are
  logged, and a script that keeps failing is disabled until it is
  reloaded.

Known gaps, accepted for now:

- The harness only covers the code paths its simulated traffic
  exercises. A hook that misbehaves only on particular input passes.
- The measurements are process-wide, so the harness must not run in
  parallel with other tests, and its budgets need slack.
- There is no hard memory cap at runtime, and the deadline can't
  interrupt one long call into a Go-implemented built-in (e.g. a huge
  `string.rep`).

If those gaps start to matter, the two next steps are a heap-sampling
watchdog that cancels a hook whose memory grows past a budget, or
switching the interpreter to [golua](https://github.com/arnodel/golua),
which has real CPU and memory accounting (marked alpha in its README).

## Alternatives considered

- **JavaScript (goja).** Viable, and gives literal `class Rat extends
  Monster` syntax, but heavier and with more surface to lock down.
- **Go's `plugin` package.** Plugins can never be unloaded and must
  match the server's exact toolchain build.
- **WASM (wazero).** The strongest sandbox, but authors need a
  compile step, which loses the edit-and-`reload` loop.
- **Starlark.** No classes, and values freeze after load.

## First slice

1. The `script` field on NPC definitions, in the loader.
2. One Lua state behind the `Load` / `CallHook` / `Unload` interface.
3. Two hooks: `on_enter` and `on_say`.
4. Two API calls: `say` and `emote`.
5. The `reload` command, with the admission harness in front of it.
6. The cellar rat (`world/npcs/cellar-rat.json`) as the first
   scripted NPC.

## Open questions

- **MCP tool registration.** Not yet checked whether
  `internal/mcpserver` can register tools after startup. It matters
  once scripts can define verbs, not for the first slice.
- **Who counts as a wizzard.** There is no auth or role system yet,
  so `reload` needs some way to be restricted.
