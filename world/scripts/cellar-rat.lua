local Rat = Monster:extend()

-- Someone walked into the cellar.
function Rat:on_enter(player)
  self:emote("bares its teeth at " .. player.name .. ".")
end

-- Rats have priorities.
function Rat:on_say(player, text)
  if text:lower():find("cheese") then
    self:emote("stops, whiskers twitching, and stares at " .. player.name .. ".")
  end
end

return Rat
