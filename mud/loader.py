"""Loads a world (rooms/items/doorways) from a directory of JSON files.

Each room lives in its own JSON file, shaped like:

    {
      "id": "cave-entrance",
      "description": "A damp cave mouth.",
      "items": [
        {"id": "torch", "name": "a torch", "description": "Still lit."}
      ],
      "doorways": [
        {"direction": "north", "to": "cave-tunnel", "description": "A narrow passage."}
      ]
    }

`id` is the only required field; everything else defaults to empty.
Doorway `to` values are validated against the set of loaded room ids once
every file has been read, so rooms can reference each other regardless of
load order.
"""

from __future__ import annotations

import json
from pathlib import Path

from .doorway import Doorway
from .item import Item
from .room import Room
from .world import World


class WorldLoadError(Exception):
    """Raised when the on-disk world data is malformed or inconsistent."""


def _room_from_dict(data: dict, *, source: Path) -> Room:
    if "id" not in data:
        raise WorldLoadError(f"{source}: room is missing required field 'id'")

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
        description=data.get("description", ""),
        items=items,
        doorways=doorways,
    )


def load_world(rooms_dir: str | Path) -> World:
    """Load every *.json file in `rooms_dir` into a World.

    Raises WorldLoadError if a file is malformed, a room id is duplicated,
    or a doorway points at a room id that doesn't exist.
    """
    rooms_dir = Path(rooms_dir)
    if not rooms_dir.is_dir():
        raise WorldLoadError(f"Not a directory: {rooms_dir}")

    world = World()

    for path in sorted(rooms_dir.glob("*.json")):
        try:
            data = json.loads(path.read_text())
        except json.JSONDecodeError as e:
            raise WorldLoadError(f"{path}: invalid JSON ({e})") from e

        room = _room_from_dict(data, source=path)

        if room.id in world.rooms:
            raise WorldLoadError(
                f"{path}: duplicate room id {room.id!r} "
                f"(already defined by another file)"
            )
        world.rooms[room.id] = room

    # Validate doorways only after every room is loaded, so order doesn't matter.
    for room in world.rooms.values():
        for doorway in room.doorways:
            if doorway.to not in world.rooms:
                raise WorldLoadError(
                    f"Room {room.id!r} has a doorway ({doorway.direction!r}) "
                    f"pointing to unknown room {doorway.to!r}"
                )

    return world
