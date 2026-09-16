"""The `help` command: lists every command usable in the player's
current room.

Two sources feed the listing:

1. The full command list (including itself), filtered to whichever
   commands' `match_room` says yes for the player's current room, shown
   via their `describe()` text. Since `match_room` is a stub that
   always returns True (see command.py), this currently just lists
   every registered command -- the room filtering has nowhere to bite
   yet.
2. Room.get_commands() -- which itself pulls in the room's doorways,
   its items' and players' own get_commands(), and the room's
   JSON-authored `commands` list. Anything in there that isn't already
   covered by a registered command above (e.g. "pick up", or a
   JSON-configured "order drink") is appended too, so `help` doesn't
   miss verbs that don't have a real Command implementation yet -- the
   same content `look`'s "you can try" hint is built from.
"""

from __future__ import annotations

import uuid

from mud.commands.command import Command, Parameter, matches_verb
from mud.room import Room
from mud.session import SessionRegistry
from mud.world import World


class HelpCommand(Command):
    def __init__(
        self, sessions: SessionRegistry, world: World, commands: list[Command]
    ) -> None:
        self._sessions = sessions
        self._world = world
        self._commands = commands

    def name(self) -> str:
        return "help"

    def match_command(self, line: str) -> bool:
        return matches_verb(line, self.name())

    def match_room(self, room: Room) -> bool:
        return True

    def describe(self) -> str:
        return "help        lists the commands you can use"

    def parameters(self) -> list[Parameter]:
        return []

    def run_command(self, line: str, session_id: uuid.UUID) -> str:
        session = self._sessions.get(session_id)
        room = self._world.get_room(session.player.current_room_id)

        matching_commands = [c for c in self._commands if c.match_room(room)]
        lines = [command.describe() for command in matching_commands]

        # Room-derived verbs (items, players, JSON `commands`) that no
        # registered command already covers -- e.g. "pick up" has no
        # Command implementation, so it wouldn't otherwise show up here.
        lines.extend(
            verb
            for verb in room.get_commands()
            if not any(c.match_command(verb) for c in matching_commands)
        )

        return "\r\n".join(lines)
