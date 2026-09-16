"""The `go` command: move through a doorway.

Takes one parameter, the dimension (direction) to go — e.g. "go north".
Matched against the player's current room's doorways by exact,
case-sensitive comparison against Doorway.direction (so "go North"
won't match a doorway whose direction is "north").

Moves the player via World.move_player (updates Room.players on both
ends too, not just player.current_room_id), then persists the move so
it survives a restart/reconnect.
"""

from __future__ import annotations

import uuid

from mud.commands.command import Command, Parameter, matches_verb
from mud.repositories.player_repository import PlayerRepository
from mud.room import Room
from mud.session import SessionRegistry
from mud.world import World


class GoCommand(Command):
    def __init__(
        self, sessions: SessionRegistry, world: World, player_repo: PlayerRepository
    ) -> None:
        self._sessions = sessions
        self._world = world
        self._player_repo = player_repo

    def name(self) -> str:
        return "go"

    def match_command(self, line: str) -> bool:
        return matches_verb(line, self.name())

    def match_room(self, room: Room) -> bool:
        return True

    def describe(self) -> str:
        return 'go <dir>    moves through a doorway (e.g. "go north")'

    def parameters(self) -> list[Parameter]:
        return [Parameter(name="direction", type=str, required=True)]

    def run_command(self, line: str, session_id: uuid.UUID) -> str:
        session = self._sessions.get(session_id)
        player = session.player

        parts = line.strip().split(maxsplit=1)
        if len(parts) < 2:
            return "Go where?"
        dimension = parts[1]

        current_room = self._world.get_room(player.current_room_id)
        doorway = next(
            (d for d in current_room.doorways if d.direction == dimension),
            None,
        )
        if doorway is None:
            return f"You can't go {dimension!r} from here."

        self._world.move_player(player, doorway.to)
        self._player_repo.save(player)
        return self._world.get_room(player.current_room_id).format()
