"""The `look` command: re-describe the player's current room.

Matches "look" plus anything after it ("look around", "look at stuff")
-- run_command ignores whatever follows for now, since there's nothing
yet to look *at* besides the whole room.

Also appends a "you can try" hint built from Room.get_commands() (which
in turn asks the room's items and players for theirs) -- a preview of
what's actionable here, not a substitute for `help`'s full list.
"""

from __future__ import annotations

import uuid

from mud.commands.command import Command, Parameter, matches_verb
from mud.room import Room
from mud.session import SessionRegistry
from mud.world import World


class LookCommand(Command):
    def __init__(self, sessions: SessionRegistry, world: World) -> None:
        self._sessions = sessions
        self._world = world

    def match_command(self, line: str) -> bool:
        return matches_verb(line, "look")

    def match_room(self, room: Room) -> bool:
        return True

    def describe(self) -> str:
        return "look        re-describes the room you're in"

    def parameters(self) -> list[Parameter]:
        return []

    def run_command(self, line: str, session_id: uuid.UUID) -> str:
        session = self._sessions.get(session_id)
        room = self._world.get_room(session.player.current_room_id)
        hint = f"You can try: {', '.join(room.get_commands())}"
        return room.format() + "\r\n" + hint
