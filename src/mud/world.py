"""World: the full set of loaded structures/rooms, plus player
placement/movement and room/structure membership."""

from __future__ import annotations

from dataclasses import dataclass, field

from mud.player import Player
from mud.room import Room
from mud.structure import Structure


@dataclass
class World:
    rooms: dict[str, Room] = field(default_factory=dict)
    structures: dict[str, Structure] = field(default_factory=dict)

    def get_room(self, room_id: str) -> Room:
        try:
            return self.rooms[room_id]
        except KeyError:
            raise KeyError(f"No such room: {room_id!r}") from None

    def get_structure(self, structure_id: str) -> Structure:
        try:
            return self.structures[structure_id]
        except KeyError:
            raise KeyError(f"No such structure: {structure_id!r}") from None

    def place_room(self, room: Room) -> None:
        """Add a room to its structure's room list.

        Use this once per room when the world loads (room.structure_id
        is already set, e.g. from JSON) — after that, use
        move_room_to_structure for any change of structure. Safe to
        call again for a room that's already placed (it's a no-op).
        """
        structure = self.get_structure(room.structure_id)
        if room not in structure.rooms:
            structure.rooms.append(room)

    def move_room_to_structure(self, room: Room, to_structure_id: str) -> None:
        """Move a room to a different structure.

        Mirrors move_player: updates room.structure_id and both
        structures' room lists together, so the two can never drift out
        of sync. Don't append/remove a room from structure.rooms
        directly.
        """
        new_structure = self.get_structure(to_structure_id)  # validate first

        old_structure = self.structures.get(room.structure_id)
        if old_structure is not None and room in old_structure.rooms:
            old_structure.rooms.remove(room)

        new_structure.rooms.append(room)
        room.structure_id = to_structure_id

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

    def remove_player(self, player: Player) -> None:
        """Remove a player from their current room's occupant list (e.g.
        on disconnect). Leaves player.current_room_id untouched, so
        reconnecting/re-placing puts them back where they were.
        """
        room = self.rooms.get(player.current_room_id)
        if room is not None and player in room.players:
            room.players.remove(player)

    def __iter__(self):
        return iter(self.rooms.values())

    def __len__(self) -> int:
        return len(self.rooms)
