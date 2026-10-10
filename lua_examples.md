# Lua Examples for Wizzards

Worked examples of scripting the MUD in Lua. The design behind this
is in `lua_scripting.md`.

Both examples here are in the game: the swamp dragon is in the
Watch's holding cells and the muttering sword is in the armory.

## The basics

Anything you script comes in two parts:

- **A JSON entry** says what the thing *is*: its name, description,
  stats. This is the same JSON the world is already written in, plus
  one new field, `"script"`.
- **A Lua file** in `world/scripts/` says what the thing *does*.

A script picks a base class, extends it, overrides the hooks it cares
about, and returns the result:

```lua
local Thing = Monster:extend()   -- or Item:extend()

function Thing:on_enter(player)  -- a hook: the engine calls this
  -- ...
end

return Thing
```

Three rules cover most of what you need:

- **Hooks are optional.** Any hook you don't write keeps the base
  class's behaviour.
- **`self` is one particular rat, not all rats.** Anything you store
  on `self` (`self.angry = true`) belongs to that one instance.
  A `local` at the top of the file is shared by every instance.
- **Call the base class if you still want what it does.** Write
  `Monster.on_attacked(self, attacker)` inside your own
  `on_attacked`.

After editing a script, type `reload <script name>` in the game
(`reload swamp-dragon`). If the new version has an error, the old one
keeps running and you are shown the error. `unload <script name>`
switches a script off: whatever was using it goes back to behaving as
if it had none.

Only wizzards can reload and unload: your name has to be in the
server's `MUD_WIZZARDS` setting.

The JSON half is only read when the server starts. Editing a script
never needs a restart, but adding `"script"` to something for the
first time does.

## Example 1: a monster

A swamp dragon: small, nervous, and prone to exploding.

The definition, in `world/npcs/swamp-dragon.json`:

```json
{
  "id": "swamp-dragon",
  "name": "A Swamp Dragon",
  "description": "About the size of a terrier, with watery eyes and the general air of a creature whose digestion is a matter of public safety.",
  "enemy": true,
  "stats": { "strength": 3, "hit_points": 10, "stamina": 6, "luck": 1, "dexterity": 4 },
  "script": "swamp-dragon.lua"
}
```

Put it in a room by adding its id to that room's `npcs` list, the
same as any other NPC:

```json
"npcs": ["swamp-dragon"]
```

The behaviour, in `world/scripts/swamp-dragon.lua`:

```lua
local Dragon = Monster:extend()

-- Someone walked into the room.
function Dragon:on_enter(player)
  self:emote("hiccups, and a small flame singes the floor near " .. player.name .. ".")
end

-- Someone spoke in the room.
function Dragon:on_say(player, text)
  if text:lower():find("coal") then
    self:emote("perks up and looks hopefully at " .. player.name .. ".")
  end
end

-- Someone hit it. Fight back as a Monster normally would, then start
-- rumbling once it is badly hurt.
function Dragon:on_attacked(attacker)
  Monster.on_attacked(self, attacker)

  if self.stats.hit_points <= 4 and not self.rumbling then
    self.rumbling = true          -- remembered by this dragon only
    self:emote("begins to make an ominous gurgling noise.")
  end
end

-- It died. Swamp dragons do not go quietly.
function Dragon:on_death(killer)
  self.room:send("The swamp dragon explodes!")
  for _, player in ipairs(self.room:players()) do
    player:damage(4)
    player:send("You are showered in bits of dragon.")
  end
end

return Dragon
```

Things to notice:

- `self.rumbling` is set on one dragon. A second swamp dragon in the
  next room has its own, and is not rumbling.
- `on_attacked` calls `Monster.on_attacked` first, so the dragon
  still fights back. Leave that line out and it would stand there
  and take it.
- `on_death` doesn't call the base class, and doesn't need to: the
  engine still removes the dragon and drops its inventory.

## Example 2: an item

A sword that complains. Items are written inline, wherever they
start out: in a room's `items` list or an NPC's `inventory` list.
Here it is lying in a room:

```json
"items": [
  {
    "id": "muttering-sword",
    "name": "a muttering sword",
    "description": "A plain blade with a faint, continuous grumble coming from somewhere near the hilt.",
    "weapon": { "damage": 4 },
    "script": "muttering-sword.lua"
  }
]
```

The behaviour, in `world/scripts/muttering-sword.lua`:

```lua
local Sword = Item:extend()

-- Shared by every muttering sword: it's a local, not on self.
local complaints = {
  "I was forged for heroes, you know.",
  "Mind the edge. No, the other edge.",
  "My last owner had a much better grip.",
}

-- Someone picked it up.
function Sword:on_pickup(player)
  self.swings = 0
  player:send("The sword sighs. \"Oh good. Another one.\"")
end

-- Its wielder landed a blow with it.
function Sword:on_hit(wielder, target)
  self.swings = self.swings + 1

  -- Every third hit, it has an opinion.
  if self.swings % 3 == 0 then
    local line = complaints[math.random(#complaints)]
    wielder.room:send("The sword mutters, \"" .. line .. "\"")
  end
end

-- Someone put it down.
function Sword:on_drop(player)
  player.room:send("The sword clatters to the floor. \"Charming.\"")
end

return Sword
```

Things to notice:

- The `"weapon"` field in the JSON is what makes it a weapon and
  sets its damage. The script only adds personality; it doesn't
  need to know about combat.
- `complaints` is shared by all muttering swords, while `self.swings`
  is counted separately by each one.
- `on_pickup` resets the count, so a sword that changes hands starts
  complaining afresh.

## Quick reference

Hooks you can write:

| Base class | Hook | Called when |
|---|---|---|
| `Monster` | `on_enter(player)` | a player walks into its room |
| `Monster` | `on_say(player, text)` | a player speaks in its room |
| `Monster` | `on_attacked(attacker)` | it has just been hit |
| `Monster` | `on_death(killer)` | it has just died |
| `Monster` | `on_tick()` | every 5 seconds, while it is alive |
| `Item` | `on_pickup(player)` | a player picks it up |
| `Item` | `on_drop(player)` | a player drops it |
| `Item` | `on_hit(wielder, target)` | its wielder lands a blow with it |

Things you can use inside a hook:

| Call | What it does |
|---|---|
| `self.name`, `self.room` | this thing's name, and the room it is in (also `self.id`, `self.description`) |
| `self.stats.hit_points` | a monster's stats (also `max_hit_points`, `strength`, `stamina`, `max_stamina`, `luck`, `dexterity`) |
| `self.damage` | a weapon's damage (`nil` for an item that isn't one) |
| `self:say(text)` | a monster says something to the room |
| `self:emote(text)` | the room sees the monster's name followed by `text` |
| `room:send(text)` | show `text` to everyone in the room |
| `room:players()` | a list of the players in the room |
| `room:spawn(id)` | spawn a new NPC from its definition, and return it (`nil` if the room is full) |
| `player.name`, `player.room`, `player.stats` | a player's name, the room they are in, and their stats |
| `player:send(text)` | show `text` to that one player |
| `player:damage(n)` | take `n` hit points off that player |

Lua's `string`, `table`, and `math` libraries are available. Files,
the network, and the operating system are not.

## Limits to know about

- A hook that runs for more than a quarter of a second is stopped,
  so don't write loops that wait for something to happen. Use
  `on_tick` instead.
- A script whose hooks fail five times in a row is switched off until
  you reload it. The server log says what went wrong.
- `print(...)` writes to the server log, not to any player.
- Before a reloaded script goes live, it is run against simulated
  traffic and rejected if it uses too much memory or keeps holding
  more of it over time. Don't collect things forever in a table.
- Nothing a script stores survives a server restart.
