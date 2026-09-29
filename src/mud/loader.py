"""Loads a world (structures/rooms/npcs/items/doorways) from a directory
tree of JSON files.

Layout:

    <world_dir>/structures/*.json   one Structure per file
    <world_dir>/rooms/*.json        one Room per file
    <world_dir>/npcs/*.json         one NPC per file (optional -- a
                                     world with no NPCs just omits this
                                     directory entirely)

Structure file, e.g. structures/old-mill.json:

    { "id": "old-mill", "name": "Old Mill", "description": "..." }

`id` and `name` are required; `description` defaults to "".

NPC file, e.g. npcs/old-tom.json:

    {
      "id": "old-tom",
      "name": "Old Tom",
      "description": "A grizzled bartender who's seen it all.",
      "hostile": false,
      "commands": ["trade"],
      "inventory": [
        {"id": "rag", "name": "a bar rag", "description": "Damp and well-used."}
      ]
    }

`id` and `name` are required; `description`/`commands`/`inventory`
default to empty and `hostile` to false. `inventory` entries are inline
Item objects, same shape as a room's `items` below.

`type` picks which NPC subclass (see npc.py) this definition builds --
"npc" (the default, if omitted) or "wandering" for a WanderingNPC, e.g.:

    {
      "id": "watch-patrol",
      "name": "A Watchman",
      "type": "wandering",
      "move_interval": 120
    }

`move_interval` (seconds; optional, defaults to WanderingNPC's own
default) is only meaningful -- and only read -- for `"type": "wandering"`.
An unrecognized `type` is a WorldLoadError.

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
      ],
      "npcs": ["old-tom"],
      "commands": ["light torch"]
    }

`id` and `structure_id` are required; everything else defaults to
empty. Doorway `to` values, room `structure_id` values, and room `npcs`
entries are all validated once every file has been read, so files can
reference each other regardless of load order.

Unlike `items` (defined inline, one-off per room), `npcs` is a list of
ids referencing npcs/*.json -- each id in `world.npcs` is the
*definition* (loaded once), and every room `npcs` entry that names it
spawns a fresh, independent NPC.spawn() instance (see npc.py) into that
room. The same id can appear in as many rooms as you like -- or more
than once in the same room's `npcs` list -- and each reference gets its
own instance: this is how one "shopkeeper" definition becomes several
independent shopkeepers standing in several different rooms.

`commands` is a plain list of extra verb strings for Room.get_commands()
-- a room-specific, JSON-configured addition to the verbs the code
already derives from the room's doorways/items/npcs/players (see
room.py).
"""

from __future__ import annotations

import json
from pathlib import Path

from .doorway import Doorway
from .item import Item
from .npc import NPC, WanderingNPC
from .room import Room
from .structure import Structure
from .world import World

# NPC JSON's "type" field -> which NPC subclass to build (see npc.py).
# "npc" is the default when the field is omitted entirely.
_NPC_TYPES: dict[str, type[NPC]] = {
    "npc": NPC,
    "wandering": WanderingNPC,
}


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


def _item_from_dict(data: dict) -> Item:
    return Item(
        id=data["id"],
        name=data.get("name", data["id"]),
        description=data.get("description", ""),
    )


def _npc_from_dict(data: dict, *, source: Path) -> NPC:
    if "id" not in data:
        raise WorldLoadError(f"{source}: npc is missing required field 'id'")
    if "name" not in data:
        raise WorldLoadError(f"{source}: npc is missing required field 'name'")

    npc_type = data.get("type", "npc")
    cls = _NPC_TYPES.get(npc_type)
    if cls is None:
        raise WorldLoadError(
            f"{source}: npc {data['id']!r} has unknown type {npc_type!r} "
            f"(expected one of {sorted(_NPC_TYPES)})"
        )

    kwargs: dict = dict(
        id=data["id"],
        name=data["name"],
        description=data.get("description", ""),
        hostile=data.get("hostile", False),
        commands=data.get("commands", []),
        inventory=[_item_from_dict(item) for item in data.get("inventory", [])],
    )
    if cls is WanderingNPC and "move_interval" in data:
        kwargs["move_interval"] = data["move_interval"]

    return cls(**kwargs)


def _room_from_dict(data: dict, *, source: Path) -> Room:
    if "id" not in data:
        raise WorldLoadError(f"{source}: room is missing required field 'id'")
    if "structure_id" not in data:
        raise WorldLoadError(
            f"{source}: room is missing required field 'structure_id'"
        )

    items = [_item_from_dict(item) for item in data.get("items", [])]

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
        commands=data.get("commands", []),
    )


def load_world(world_dir: str | Path) -> World:
    """Load <world_dir>/structures/*.json, <world_dir>/rooms/*.json, and
    (if present) <world_dir>/npcs/*.json into a World.

    Raises WorldLoadError if a file is malformed, an id is duplicated, a
    doorway points at an unknown room, a room references an unknown
    structure_id, a room references an unknown npc id, or an npc has an
    unrecognized `type`.
    """
    world_dir = Path(world_dir)
    structures_dir = world_dir / "structures"
    rooms_dir = world_dir / "rooms"
    npcs_dir = world_dir / "npcs"

    if not structures_dir.is_dir():
        raise WorldLoadError(f"Not a directory: {structures_dir}")
    if not rooms_dir.is_dir():
        raise WorldLoadError(f"Not a directory: {rooms_dir}")
    # npcs_dir is optional -- a world with no NPCs at all just omits it.

    world = World()

    for path, data in _load_json_files(structures_dir):
        structure = _structure_from_dict(data, source=path)
        if structure.id in world.structures:
            raise WorldLoadError(
                f"{path}: duplicate structure id {structure.id!r} "
                f"(already defined by another file)"
            )
        world.structures[structure.id] = structure

    if npcs_dir.is_dir():
        for path, data in _load_json_files(npcs_dir):
            npc = _npc_from_dict(data, source=path)
            if npc.id in world.npcs:
                raise WorldLoadError(
                    f"{path}: duplicate npc id {npc.id!r} "
                    f"(already defined by another file)"
                )
            world.npcs[npc.id] = npc

    # Room `npcs` entries are ids, resolved below once every npcs/*.json
    # has been read -- stashed here (keyed by room id) rather than on
    # Room itself, since Room.npcs holds actual NPC objects, not ids.
    room_npc_ids: dict[str, list[str]] = {}

    for path, data in _load_json_files(rooms_dir):
        room = _room_from_dict(data, source=path)
        if room.id in world.rooms:
            raise WorldLoadError(
                f"{path}: duplicate room id {room.id!r} "
                f"(already defined by another file)"
            )
        world.rooms[room.id] = room
        room_npc_ids[room.id] = data.get("npcs", [])

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

        for npc_id in room_npc_ids[room.id]:
            if npc_id not in world.npcs:
                raise WorldLoadError(
                    f"Room {room.id!r} references unknown npc {npc_id!r}"
                )
            # A fresh instance per reference, not the shared definition
            # -- see NPC.spawn() and the module docstring above.
            room.npcs.append(world.npcs[npc_id].spawn(room.id))

        world.place_room(room)

    return world
