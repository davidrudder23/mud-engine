-- The base classes every script extends. Runs once, when an engine
-- starts, in the real global table; the Go side then adds the methods
-- that need the engine (Monster.say, Monster.emote, Monster.on_attacked).

local Object = {}
Object.__index = Object

-- A new class inheriting from this one: anything it doesn't define
-- itself is looked up here.
function Object:extend()
  local class = {}
  class.__index = class
  class.super = self
  return setmetatable(class, self)
end

-- Every hook exists on the base class as a no-op, so a script can
-- always call the base version (Monster.on_enter(self, player)).

Monster = Object:extend()
function Monster:on_enter(player) end
function Monster:on_say(player, text) end
function Monster:on_death(killer) end
function Monster:on_tick() end

Item = Object:extend()
function Item:on_pickup(player) end
function Item:on_drop(player) end
function Item:on_hit(wielder, target) end
