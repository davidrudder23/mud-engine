"""The `pick up` command: move an item from the current room into the
player's inventory.

Matches items by a case-insensitive substring against either the
item's id or its name, so "pick up tankard" finds an item whose id is
"tankard" and "pick up half-finished" finds one named "a half-finished
tankard" -- same spirit as `go`'s prefix matching, just applied to item
names instead of directions.
"""

from __future__ import annotations

import uuid

from mud.commands.command import Command, Parameter, command_argument, matches_verb
from mud.room import Room
from mud.session import SessionRegistry
from mud.world import World


class PickUpCommand(Command):
    def __init__(self, sessions: SessionRegistry, world: World) -> None:
        self._sessions = sessions
        self._world = world

    def match_command(self, line: str) -> bool:
        return matches_verb(line, "pick up")

    def match_room(self, room: Room) -> bool:
        return True

    def describe(self) -> str:
        return "pick up <item>   picks up an item from the room"

    def parameters(self) -> list[Parameter]:
        return [Parameter(name="item", type=str, required=True)]

    def run_command(self, line: str, session_id: uuid.UUID) -> str:
        session = self._sessions.get(session_id)
        player = session.player
        room = self._world.get_room(player.current_room_id)

        query = command_argument(line, "pick up").lower()
        if not query:
            return "Pick up what?"

        item = next(
            (i for i in room.items if query in i.id.lower() or query in i.name.lower()),
            None,
        )
        if item is None:
            return f"You don't see {query!r} here."

        room.items.remove(item)
        player.inventory.append(item)
        return f"You pick up {item.format()}."
