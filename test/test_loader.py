"""Tests for mud.loader.load_world: reading a directory of structure/room
JSON files into a World, and rejecting malformed or inconsistent data."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from mud.loader import WorldLoadError, load_world


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


if __name__ == "__main__":
    unittest.main()
