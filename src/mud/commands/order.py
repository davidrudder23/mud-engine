"""The `order` command: order something from whoever's serving.

Pure flavor for now -- no drink inventory, no gold, no bartender to
actually check with. It just echoes back what was ordered, the same
way `go` used to just return "lorem ipsum" before doorways were real.
Takes no room/session state, since there's nothing yet to act on.
"""

from __future__ import annotations

import uuid

from mud.commands.command import Command, Parameter, command_argument, matches_verb
from mud.room import Room


class OrderCommand(Command):
    def match_command(self, line: str) -> bool:
        return matches_verb(line, "order")

    def match_room(self, room: Room) -> bool:
        return True

    def describe(self) -> str:
        return "order <drink>   orders a drink from whoever's serving"

    def parameters(self) -> list[Parameter]:
        return [Parameter(name="drink", type=str, required=True)]

    def run_command(self, line: str, session_id: uuid.UUID) -> str:
        item = command_argument(line, "order")
        if not item:
            return "Order what?"
        return f"You order {item}. It's set down in front of you without much ceremony."
