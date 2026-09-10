"""World: the full set of loaded rooms, plus player placement/movement."""

from __future__ import annotations

from dataclasses import dataclass, field

from mud.player import Player
from mud.room import Room


@dataclass
class World:
    rooms: dict[str, Room] = field(default_factory=dict)

    def get_room(self, room_id: str) -> Room:
        try:
            return self.rooms[room_id]
        except KeyError:
            raise KeyError(f"No such room: {room_id!r}") from None

    def place_player(self, player: Player) -> None:
        """Add a player to its current room's occupant list.

        Use this once per player when they first join a running world
        (e.g. on connect, using their persisted current_room_id) — after
        that, use move_player for any change of room. Safe to call again
        for a player who's already placed (it's a no-op).
        """
        room = self.get_room(player.current_room_id)
        if player not in room.players:
            room.players.append(player)

    def move_player(self, player: Player, to_room_id: str) -> None:
        """Move a player to a different room.

        This is the *only* thing that should change which room a player
        is in: it updates player.current_room_id and both rooms'
        occupant lists together, so the two can never drift out of sync.
        Don't append/remove a player from room.players directly.

        Note: this only updates in-memory state. If the player's room
        needs to survive a restart, persist it too (e.g. via
        PlayerRepository.save) after calling this.
        """
        new_room = self.get_room(to_room_id)  # validate before mutating anything

        old_room = self.rooms.get(player.current_room_id)
        if old_room is not None and player in old_room.players:
            old_room.players.remove(player)

        new_room.players.append(player)
        player.current_room_id = to_room_id

    def __iter__(self):
        return iter(self.rooms.values())

    def __len__(self) -> int:
        return len(self.rooms)
