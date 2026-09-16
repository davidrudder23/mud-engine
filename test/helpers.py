"""Small world-building helpers shared by tests that don't care about
JSON loading itself (that's what test_loader.py is for) and just need a
minimal, known-shape World to act on."""

from __future__ import annotations

from mud.doorway import Doorway
from mud.room import Room
from mud.structure import Structure
from mud.world import World


def build_two_room_world() -> tuple[World, str, str]:
    """A two-room world, "start" <-> "north-room", connected both ways.
    Returns (world, start_room_id, north_room_id)."""
    world = World()
    structure = Structure(id="test-structure", name="Test Structure")
    world.structures[structure.id] = structure

    start = Room(
        id="start",
        structure_id=structure.id,
        description="The starting room.",
        doorways=[Doorway(direction="north", to="north-room")],
    )
    north_room = Room(
        id="north-room",
        structure_id=structure.id,
        description="A room to the north.",
        doorways=[Doorway(direction="south", to="start")],
    )

    for room in (start, north_room):
        world.rooms[room.id] = room
        world.place_room(room)

    return world, start.id, north_room.id
