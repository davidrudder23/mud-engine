"""The `quit` command: closes the connection.

Takes no arguments, so run_command ignores anything after the verb.
Commands only return text — they don't have the writer — so this works
by setting a flag on the session; telnet_server's read loop checks it
after each command and closes the connection if it's set.
"""

from __future__ import annotations

import uuid

from mud.commands.command import Command, Parameter, matches_verb
from mud.room import Room
from mud.session import SessionRegistry


class QuitCommand(Command):
    def __init__(self, sessions: SessionRegistry) -> None:
        self._sessions = sessions

    def match_command(self, line: str) -> bool:
        return matches_verb(line, "quit")

    def match_room(self, room: Room) -> bool:
        return True

    def describe(self) -> str:
        return "quit        closes the connection"

    def parameters(self) -> list[Parameter]:
        return []

    def run_command(self, line: str, session_id: uuid.UUID) -> str:
        session = self._sessions.get(session_id)
        session.should_close = True
        return ""
