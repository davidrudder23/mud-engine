"""Tests for mud.loader.load_world: reading a directory of structure/room
JSON files into a World, and rejecting malformed or inconsistent data."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from mud.loader import WorldLoadError, load_world
from mud.npc import NPC, WanderingNPC


def _write(path: Path, data: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data))


class LoaderTestCase(unittest.TestCase):
    """Base class that gives each test a scratch <tmp>/world directory."""

    def setUp(self) -> None:
        self._tmpdir = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmpdir.cleanup)
        self.world_dir = Path(self._tmpdir.name)
        (self.world_dir / "structures").mkdir()
        (self.world_dir / "rooms").mkdir()

    def write_structure(self, filename: str, data: dict) -> None:
        _write(self.world_dir / "structures" / filename, data)

    def write_room(self, filename: str, data: dict) -> None:
        _write(self.world_dir / "rooms" / filename, data)

    def write_npc(self, filename: str, data: dict) -> None:
        # No npcs/ dir in setUp (it's optional) -- _write's mkdir(parents=True)
        # creates it on first use, same as it would for structures/rooms.
        _write(self.world_dir / "npcs" / filename, data)


class HappyPathTests(LoaderTestCase):
    def test_loads_structures_and_rooms(self) -> None:
        self.write_structure(
            "old-mill.json",
            {"id": "old-mill", "name": "Old Mill", "description": "A creaky mill."},
        )
        self.write_room(
            "cave-entrance.json",
            {
                "id": "cave-entrance",
                "structure_id": "old-mill",
                "description": "A damp cave mouth.",
                "items": [{"id": "torch", "name": "a torch", "description": "Lit."}],
                "doorways": [
                    {"direction": "north", "to": "cave-tunnel", "description": "Narrow."}
                ],
            },
        )
        self.write_room(
            "cave-tunnel.json", {"id": "cave-tunnel", "structure_id": "old-mill"}
        )

        world = load_world(self.world_dir)

        self.assertEqual(set(world.structures), {"old-mill"})
        self.assertEqual(set(world.rooms), {"cave-entrance", "cave-tunnel"})

        entrance = world.get_room("cave-entrance")
        self.assertEqual(entrance.description, "A damp cave mouth.")
        self.assertEqual(len(entrance.items), 1)
        self.assertEqual(entrance.items[0].id, "torch")
        self.assertEqual(entrance.doorways[0].to, "cave-tunnel")

        # Rooms are linked into their structure by load_world (via
        # World.place_room), not left for the caller to wire up.
        structure = world.get_structure("old-mill")
        self.assertCountEqual(structure.rooms, [entrance, world.get_room("cave-tunnel")])

    def test_defaults_for_optional_fields(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_room("r.json", {"id": "r", "structure_id": "s"})

        world = load_world(self.world_dir)

        structure = world.get_structure("s")
        room = world.get_room("r")
        self.assertEqual(structure.description, "")
        self.assertEqual(room.description, "")
        self.assertEqual(room.items, [])
        self.assertEqual(room.doorways, [])
        self.assertEqual(room.players, [])  # never set from JSON
        self.assertEqual(room.commands, [])

    def test_loads_room_commands(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_room(
            "r.json",
            {"id": "r", "structure_id": "s", "commands": ["light torch", "pray"]},
        )

        world = load_world(self.world_dir)

        self.assertEqual(world.get_room("r").commands, ["light torch", "pray"])

    def test_item_name_defaults_to_id(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_room(
            "r.json",
            {"id": "r", "structure_id": "s", "items": [{"id": "rock"}]},
        )

        world = load_world(self.world_dir)

        self.assertEqual(world.get_room("r").items[0].name, "rock")

    def test_load_order_does_not_matter_for_forward_references(self) -> None:
        # room-a's doorway points at room-b, but room-a's file is read
        # first -- load_world must not validate until everything is in.
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_room(
            "a.json",
            {
                "id": "room-a",
                "structure_id": "s",
                "doorways": [{"direction": "east", "to": "room-b"}],
            },
        )
        self.write_room("b.json", {"id": "room-b", "structure_id": "s"})

        world = load_world(self.world_dir)

        self.assertIn("room-b", world.rooms)
        self.assertEqual(world.get_room("room-a").doorways[0].to, "room-b")

    def test_missing_npcs_directory_is_not_an_error(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_room("r.json", {"id": "r", "structure_id": "s"})

        world = load_world(self.world_dir)

        self.assertEqual(world.npcs, {})
        self.assertEqual(world.get_room("r").npcs, [])

    def test_loads_npcs_and_links_them_into_referencing_rooms(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc(
            "old-tom.json",
            {"id": "old-tom", "name": "Old Tom", "description": "A bartender."},
        )
        self.write_room(
            "r.json", {"id": "r", "structure_id": "s", "npcs": ["old-tom"]}
        )

        world = load_world(self.world_dir)

        self.assertEqual(set(world.npcs), {"old-tom"})
        npc = world.get_npc("old-tom")
        self.assertEqual(npc.name, "Old Tom")
        self.assertEqual(npc.description, "A bartender.")

        room = world.get_room("r")
        self.assertEqual(len(room.npcs), 1)
        # A spawned instance, not the definition itself -- same field
        # values, but its own independent object (see NPC.spawn()).
        self.assertIsNot(room.npcs[0], npc)
        self.assertEqual(room.npcs[0].name, "Old Tom")
        self.assertEqual(room.npcs[0].description, "A bartender.")
        self.assertEqual(room.npcs[0].room_id, "r")
        self.assertIsNone(npc.room_id)  # the definition isn't standing anywhere

    def test_same_npc_id_placed_in_multiple_rooms_are_independent_instances(self) -> None:
        # "imagine shopkeepers in every area" -- one definition, spawned
        # into several rooms, each fully independent of the others.
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc(
            "shopkeeper.json",
            {"id": "shopkeeper", "name": "Shopkeeper", "inventory": [{"id": "rag"}]},
        )
        self.write_room(
            "a.json", {"id": "room-a", "structure_id": "s", "npcs": ["shopkeeper"]}
        )
        self.write_room(
            "b.json", {"id": "room-b", "structure_id": "s", "npcs": ["shopkeeper"]}
        )

        world = load_world(self.world_dir)

        npc_a = world.get_room("room-a").npcs[0]
        npc_b = world.get_room("room-b").npcs[0]
        self.assertIsNot(npc_a, npc_b)
        self.assertEqual(npc_a.name, npc_b.name)
        self.assertEqual(npc_a.room_id, "room-a")
        self.assertEqual(npc_b.room_id, "room-b")

        # Selling off room-a's shopkeeper's stock doesn't touch room-b's.
        npc_a.inventory.clear()
        self.assertEqual(len(npc_b.inventory), 1)

    def test_same_npc_id_placed_twice_in_one_room_are_independent_instances(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc("guard.json", {"id": "guard", "name": "Guard"})
        self.write_room(
            "r.json",
            {"id": "r", "structure_id": "s", "npcs": ["guard", "guard"]},
        )

        room = load_world(self.world_dir).get_room("r")

        self.assertEqual(len(room.npcs), 2)
        self.assertIsNot(room.npcs[0], room.npcs[1])

    def test_spawned_instance_is_independent_of_the_definition(self) -> None:
        # Mutating a spawned instance must not leak back onto the
        # definition (which the next spawn() would otherwise copy from).
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc(
            "old-tom.json",
            {"id": "old-tom", "name": "Old Tom", "inventory": [{"id": "rag"}]},
        )
        self.write_room(
            "r.json", {"id": "r", "structure_id": "s", "npcs": ["old-tom"]}
        )

        world = load_world(self.world_dir)
        world.get_room("r").npcs[0].inventory.clear()

        self.assertEqual(len(world.get_npc("old-tom").inventory), 1)

    def test_npc_defaults_for_optional_fields(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc("old-tom.json", {"id": "old-tom", "name": "Old Tom"})
        self.write_room(
            "r.json", {"id": "r", "structure_id": "s", "npcs": ["old-tom"]}
        )

        npc = load_world(self.world_dir).get_npc("old-tom")

        self.assertEqual(npc.description, "")
        self.assertFalse(npc.hostile)
        self.assertEqual(npc.commands, [])
        self.assertEqual(npc.inventory, [])

    def test_loads_npc_inventory(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc(
            "old-tom.json",
            {
                "id": "old-tom",
                "name": "Old Tom",
                "hostile": True,
                "commands": ["trade"],
                "inventory": [{"id": "rag", "description": "Damp."}],
            },
        )
        self.write_room(
            "r.json", {"id": "r", "structure_id": "s", "npcs": ["old-tom"]}
        )

        npc = load_world(self.world_dir).get_npc("old-tom")

        self.assertTrue(npc.hostile)
        self.assertEqual(npc.commands, ["trade"])
        self.assertEqual(len(npc.inventory), 1)
        self.assertEqual(npc.inventory[0].id, "rag")
        self.assertEqual(npc.inventory[0].name, "rag")  # defaults to id
        self.assertEqual(npc.inventory[0].description, "Damp.")

    def test_a_room_with_no_npcs_field_gets_an_empty_list(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_room("r.json", {"id": "r", "structure_id": "s"})

        world = load_world(self.world_dir)

        self.assertEqual(world.get_room("r").npcs, [])

    def test_npc_type_defaults_to_plain_npc(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc("old-tom.json", {"id": "old-tom", "name": "Old Tom"})
        self.write_room("r.json", {"id": "r", "structure_id": "s", "npcs": ["old-tom"]})

        world = load_world(self.world_dir)

        self.assertIs(type(world.get_npc("old-tom")), NPC)
        self.assertIs(type(world.get_room("r").npcs[0]), NPC)

    def test_wandering_npc_type_builds_a_wandering_npc(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc(
            "watchman.json",
            {"id": "watchman", "name": "A Watchman", "type": "wandering"},
        )
        self.write_room(
            "r.json", {"id": "r", "structure_id": "s", "npcs": ["watchman"]}
        )

        world = load_world(self.world_dir)

        definition = world.get_npc("watchman")
        self.assertIsInstance(definition, WanderingNPC)
        self.assertEqual(definition.move_interval, 120.0)  # WanderingNPC's default

        instance = world.get_room("r").npcs[0]
        self.assertIsInstance(instance, WanderingNPC)
        self.assertEqual(instance.room_id, "r")

    def test_wandering_npc_move_interval_is_configurable(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc(
            "watchman.json",
            {
                "id": "watchman", "name": "A Watchman", "type": "wandering",
                "move_interval": 45,
            },
        )
        self.write_room(
            "r.json", {"id": "r", "structure_id": "s", "npcs": ["watchman"]}
        )

        world = load_world(self.world_dir)

        self.assertEqual(world.get_npc("watchman").move_interval, 45)
        self.assertEqual(world.get_room("r").npcs[0].move_interval, 45)

    def test_move_interval_is_ignored_for_a_plain_npc(self) -> None:
        # Only meaningful for "type": "wandering" -- a stray
        # move_interval on a plain npc is silently unused, not an error.
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc(
            "old-tom.json",
            {"id": "old-tom", "name": "Old Tom", "move_interval": 45},
        )
        self.write_room("r.json", {"id": "r", "structure_id": "s", "npcs": ["old-tom"]})

        world = load_world(self.world_dir)

        self.assertIs(type(world.get_npc("old-tom")), NPC)


class ValidationErrorTests(LoaderTestCase):
    def test_missing_structures_directory(self) -> None:
        import shutil

        shutil.rmtree(self.world_dir / "structures")
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_missing_rooms_directory(self) -> None:
        import shutil

        shutil.rmtree(self.world_dir / "rooms")
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_malformed_json(self) -> None:
        (self.world_dir / "structures" / "broken.json").write_text("{not json")
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_structure_missing_id(self) -> None:
        self.write_structure("s.json", {"name": "S"})
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_structure_missing_name(self) -> None:
        self.write_structure("s.json", {"id": "s"})
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_room_missing_id(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_room("r.json", {"structure_id": "s"})
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_room_missing_structure_id(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_room("r.json", {"id": "r"})
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_duplicate_structure_id_across_files(self) -> None:
        self.write_structure("s1.json", {"id": "dup", "name": "One"})
        self.write_structure("s2.json", {"id": "dup", "name": "Two"})
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_duplicate_room_id_across_files(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_room("r1.json", {"id": "dup", "structure_id": "s"})
        self.write_room("r2.json", {"id": "dup", "structure_id": "s"})
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_doorway_to_unknown_room(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_room(
            "r.json",
            {
                "id": "r",
                "structure_id": "s",
                "doorways": [{"direction": "north", "to": "nowhere"}],
            },
        )
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_room_references_unknown_structure(self) -> None:
        self.write_room("r.json", {"id": "r", "structure_id": "ghost-structure"})
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_npc_missing_id(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc("old-tom.json", {"name": "Old Tom"})
        self.write_room("r.json", {"id": "r", "structure_id": "s"})
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_npc_missing_name(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc("old-tom.json", {"id": "old-tom"})
        self.write_room("r.json", {"id": "r", "structure_id": "s"})
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_npc_unknown_type(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc(
            "old-tom.json", {"id": "old-tom", "name": "Old Tom", "type": "flying"}
        )
        self.write_room("r.json", {"id": "r", "structure_id": "s"})
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_duplicate_npc_id_across_files(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_npc("a.json", {"id": "dup", "name": "One"})
        self.write_npc("b.json", {"id": "dup", "name": "Two"})
        self.write_room("r.json", {"id": "r", "structure_id": "s"})
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)

    def test_room_references_unknown_npc(self) -> None:
        self.write_structure("s.json", {"id": "s", "name": "S"})
        self.write_room(
            "r.json", {"id": "r", "structure_id": "s", "npcs": ["ghost-npc"]}
        )
        with self.assertRaises(WorldLoadError):
            load_world(self.world_dir)


if __name__ == "__main__":
    unittest.main()
