"""Loads a world (structures/rooms/items/doorways) from a directory tree
of JSON files.

Layout:

    <world_dir>/structures/*.json   one Structure per file
    <world_dir>/rooms/*.json        one Room per file

Structure file, e.g. structures/old-mill.json:

    { "id": "old-mill", "name": "Old Mill", "description": "..." }

`id` and `name` are required; `description` defaults to "".

Room file, e.g. rooms/cave-entrance.json:

    {
      "id": "cave-entrance",
      "structure_id": "old-mill",
      "description": "A damp cave mouth.",
      "items": [
        {"id": "torch", "name": "a torch", "description": "Still lit."}
      ],
      "doorways": [
        {"direction": "north", "to": "cave-tunnel", "description": "A narrow passage."}
      ]
    }

`id` and `structure_id` are required; everything else defaults to
empty. Doorway `to` values and room `structure_id` values are both
validated once every file has been read, so files can reference each
other regardless of load order.
"""

from __future__ import annotations

import json
from pathlib import Path

from .doorway import Doorway
from .item import Item
from .room import Room
from .structure import Structure
from .world import World


class WorldLoadError(Exception):
    """Raised when the on-disk world data is malformed or inconsistent."""


def _load_json_files(directory: Path):
    for path in sorted(directory.glob("*.json")):
        try:
            yield path, json.loads(path.read_text())
        except json.JSONDecodeError as e:
            raise WorldLoadError(f"{path}: invalid JSON ({e})") from e


def _structure_from_dict(data: dict, *, source: Path) -> Structure:
    if "id" not in data:
        raise WorldLoadError(f"{source}: structure is missing required field 'id'")
    if "name" not in data:
        raise WorldLoadError(f"{source}: structure is missing required field 'name'")

    return Structure(
        id=data["id"],
        name=data["name"],
        description=data.get("description", ""),
    )


def _room_from_dict(data: dict, *, source: Path) -> Room:
    if "id" not in data:
        raise WorldLoadError(f"{source}: room is missing required field 'id'")
    if "structure_id" not in data:
        raise WorldLoadError(
            f"{source}: room is missing required field 'structure_id'"
        )

    items = [
        Item(
            id=item["id"],
            name=item.get("name", item["id"]),
            description=item.get("description", ""),
        )
        for item in data.get("items", [])
    ]

    doorways = [
        Doorway(
            direction=doorway["direction"],
            to=doorway["to"],
            description=doorway.get("description", ""),
        )
        for doorway in data.get("doorways", [])
    ]

    return Room(
        id=data["id"],
        structure_id=data["structure_id"],
        description=data.get("description", ""),
        items=items,
        doorways=doorways,
    )


def load_world(world_dir: str | Path) -> World:
    """Load <world_dir>/structures/*.json and <world_dir>/rooms/*.json
    into a World.

    Raises WorldLoadError if a file is malformed, an id is duplicated, a
    doorway points at an unknown room, or a room references an unknown
    structure_id.
    """
    world_dir = Path(world_dir)
    structures_dir = world_dir / "structures"
    rooms_dir = world_dir / "rooms"

    if not structures_dir.is_dir():
        raise WorldLoadError(f"Not a directory: {structures_dir}")
    if not rooms_dir.is_dir():
        raise WorldLoadError(f"Not a directory: {rooms_dir}")

    world = World()

    for path, data in _load_json_files(structures_dir):
        structure = _structure_from_dict(data, source=path)
        if structure.id in world.structures:
            raise WorldLoadError(
                f"{path}: duplicate structure id {structure.id!r} "
                f"(already defined by another file)"
            )
        world.structures[structure.id] = structure

    for path, data in _load_json_files(rooms_dir):
        room = _room_from_dict(data, source=path)
        if room.id in world.rooms:
            raise WorldLoadError(
                f"{path}: duplicate room id {room.id!r} "
                f"(already defined by another file)"
            )
        world.rooms[room.id] = room

    # Validate & link only after everything is loaded, so file order
    # doesn't matter.
    for room in world.rooms.values():
        for doorway in room.doorways:
            if doorway.to not in world.rooms:
                raise WorldLoadError(
                    f"Room {room.id!r} has a doorway ({doorway.direction!r}) "
                    f"pointing to unknown room {doorway.to!r}"
                )

        if room.structure_id not in world.structures:
            raise WorldLoadError(
                f"Room {room.id!r} belongs to unknown structure "
                f"{room.structure_id!r}"
            )
        world.place_room(room)

    return world
