"""Room: a location, with static content (description, items, doorways)
and live occupancy (players)."""

from __future__ import annotations

from dataclasses import dataclass, field

from mud.doorway import Doorway
from mud.item import Item
from mud.player import Player


@dataclass
class Room:
    id: str
    description: str = ""
    items: list[Item] = field(default_factory=list)
    doorways: list[Doorway] = field(default_factory=list)
    # Who's currently here. Unlike the fields above (static, authored
    # content from JSON), this is live, in-memory occupancy — it starts
    # empty even though the JSON file never sets it, and it's only ever
    # mutated via World.place_player / World.move_player, never directly.
    players: list[Player] = field(default_factory=list)

    def format(self) -> str:
        """Render as telnet-friendly text (CRLF line endings)."""
        lines = [f"[{self.id}]", self.description]

        if self.items:
            lines.append("You see:")
            lines.extend(f"  - {item.format()}" for item in self.items)

        if self.doorways:
            lines.append("Doorways:")
            lines.extend(f"  - {doorway.format()}" for doorway in self.doorways)

        return "\r\n".join(lines)
