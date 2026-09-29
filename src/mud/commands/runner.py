"""CommandRunner: matches a line of input against known commands and
runs whichever one matches first."""

from __future__ import annotations

import logging
import uuid

from mud.commands.command import Command

logger = logging.getLogger(__name__)


class CommandRunner:
    def __init__(self, commands: list[Command]) -> None:
        self._commands = commands

    def run(self, line: str, session_id: uuid.UUID) -> str:
        for command in self._commands:
            if command.match_command(line):
                return command.run_command(line, session_id)
        logger.debug("No command matched: %r", line)
        return f"Unknown command: {line.strip()!r}"
