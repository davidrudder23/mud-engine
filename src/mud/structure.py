"""Structure: a building, cavern, or other container that groups rooms
together (e.g. "Old Mill", "Sunken Cave"). Every Room belongs to
exactly one Structure; a Structure can contain many Rooms.
"""

from __future__ import annotations

from dataclasses import dataclass, field

from mud.room import Room


@dataclass
class Structure:
    id: str
    name: str
    description: str = ""
    # Kept in sync with each room's structure_id via
    # World.place_room / World.move_room_to_structure — nothing else
    # should touch this list directly.
    rooms: list[Room] = field(default_factory=list)

    def format(self) -> str:
        return f"{self.name}\r\n{self.description}"
