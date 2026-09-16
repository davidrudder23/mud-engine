"""The `drop` command: move an item from the player's inventory back
into the current room -- the reverse of `pick up` (see pick_up.py).

Matches items the same way `pick up` does: a case-insensitive substring
against the item's id or name, but searched against the player's
inventory instead of the room.
"""

from __future__ import annotations

import uuid

from mud.commands.command import Command, Parameter, command_argument, matches_verb
from mud.room import Room
from mud.session import SessionRegistry
from mud.world import World


class DropCommand(Command):
    def __init__(self, sessions: SessionRegistry, world: World) -> None:
        self._sessions = sessions
        self._world = world

    def name(self) -> str:
        return "drop"

    def match_command(self, line: str) -> bool:
        return matches_verb(line, self.name())

    def match_room(self, room: Room) -> bool:
        return True

    def describe(self) -> str:
        return "drop <item>      drops an item from your inventory into the room"

    def parameters(self) -> list[Parameter]:
        return [Parameter(name="item", type=str, required=True)]

    def run_command(self, line: str, session_id: uuid.UUID) -> str:
        session = self._sessions.get(session_id)
        player = session.player
        room = self._world.get_room(player.current_room_id)

        query = command_argument(line, "drop").lower()
        if not query:
            return "Drop what?"

        item = next(
            (
                i
                for i in player.inventory
                if query in i.id.lower() or query in i.name.lower()
            ),
            None,
        )
        if item is None:
            return f"You aren't carrying {query!r}."

        player.inventory.remove(item)
        room.items.append(item)
        return f"You drop {item.format()}."
