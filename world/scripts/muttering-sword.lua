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
