"""Room: a location, with static content (description, items, doorways)
and live occupancy (players)."""

from __future__ import annotations

from dataclasses import dataclass, field

from mud.doorway import Doorway
from mud.item import Item
from mud.npc import NPC
from mud.player import Player


@dataclass
class Room:
    id: str
    # Every room belongs to exactly one structure. Kept in sync with
    # Structure.rooms via World.place_room / World.move_room_to_structure
    # — don't set this directly.
    structure_id: str
    description: str = ""
    items: list[Item] = field(default_factory=list)
    doorways: list[Doorway] = field(default_factory=list)
    # NPCs placed in this room. Referenced by id from this room's JSON
    # rather than embedded inline like items are (see loader.py), but
    # each entry loads as its own independent NPC.spawn() instance, not
    # a shared reference to World.npcs' definition -- so the same
    # "shopkeeper" id can be placed in many rooms (or more than once in
    # this one) as fully separate NPCs, each with its own inventory.
    # Mutating one has no effect on any other spawn, or on the
    # definition itself.
    npcs: list[NPC] = field(default_factory=list)
    # Who's currently here. Unlike the fields above (static, authored
    # content from JSON), this is live, in-memory occupancy — it starts
    # empty even though the JSON file never sets it, and it's only ever
    # mutated via World.place_player / World.move_player, never directly.
    players: list[Player] = field(default_factory=list)
    # Extra verbs authored directly on the room in JSON ("commands":
    # [...]), on top of whatever get_commands() derives from the room's
    # own content below. This is the second, configurable way to make a
    # command show up in a room, alongside the code-driven defaults
    # (always "look", "go" when there's a doorway, and whatever items/
    # players contribute) -- deliberately two ways to define one, so a
    # one-off room can offer a verb without a matching item/player.
    commands: list[str] = field(default_factory=list)

    def format(self) -> str:
        """Render as telnet-friendly text (CRLF line endings)."""
        lines = [f"[{self.id}]", self.description]

        if self.items:
            lines.append("You see:")
            lines.extend(f"  - {item.format()}" for item in self.items)

        if self.npcs:
            lines.append("NPCs here:")
            lines.extend(f"  - {npc.format()}" for npc in self.npcs)

        if self.players:
            lines.append("Players here:")
            lines.extend(f"  - {player.format()}" for player in self.players)

        if self.doorways:
            lines.append("Doorways:")
            lines.extend(f"  - {doorway.format()}" for doorway in self.doorways)

        return "\r\n".join(lines)

    def to_dict(self) -> dict:
        """JSON-serializable snapshot, for the MCP server (see
        mcp_server.py) -- the structured counterpart to format(), with
        get_commands()'s "you can try" list folded in too."""
        return {
            "id": self.id,
            "structure_id": self.structure_id,
            "description": self.description,
            "items": [item.to_dict() for item in self.items],
            "npcs": [npc.to_dict() for npc in self.npcs],
            "players": [player.to_dict() for player in self.players],
            "doorways": [doorway.to_dict() for doorway in self.doorways],
            "commands": self.get_commands(),
        }

    def get_commands(self) -> list[str]:
        """Verbs available in this room right now, for `look`'s "you can
        try" hint: always "look"; "go" if there's a doorway; whatever
        the items, NPCs, and players present each contribute; plus
        whatever this room's JSON authored directly onto `commands`.
        Deduplicated, order preserved (e.g. two items both offering
        "pick up", or a JSON "commands" entry repeating a default,
        shouldn't list it twice).
        """
        commands = ["look"]

        if self.doorways:
            commands.append("go")

        for item in self.items:
            commands.extend(item.get_commands())

        for npc in self.npcs:
            commands.extend(npc.get_commands())

        for player in self.players:
            commands.extend(player.get_commands())

        commands.extend(self.commands)

        return list(dict.fromkeys(commands))
