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
