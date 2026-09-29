"""Tests for Room.get_commands() (and the Item/Player/NPC
get_commands() it delegates to): the "you can try" verbs look/help
build their listings from -- and for to_dict() (Room and everything it
holds), the structured, JSON-serializable counterpart to format() the
MCP server (mcp_server.py) returns from every tool call."""

from __future__ import annotations

import unittest
import uuid

from mud.doorway import Doorway
from mud.item import Item
from mud.npc import NPC
from mud.player import Player
from mud.room import Room


def _room(**kwargs) -> Room:
    return Room(id="room", structure_id="structure", **kwargs)


class ItemCommandsTests(unittest.TestCase):
    def test_item_offers_pick_up(self) -> None:
        item = Item(id="sword", name="a sword")
        self.assertEqual(item.get_commands(), ["pick up"])


class PlayerCommandsTests(unittest.TestCase):
    def test_player_offers_nothing_yet(self) -> None:
        player = Player(name="alice", current_room_id="room")
        self.assertEqual(player.get_commands(), [])


class NPCCommandsTests(unittest.TestCase):
    def test_npc_offers_talk(self) -> None:
        npc = NPC(id="old-tom", name="Old Tom")
        self.assertEqual(npc.get_commands(), ["talk"])


class RoomCommandsTests(unittest.TestCase):
    def test_empty_room_only_offers_look(self) -> None:
        self.assertEqual(_room().get_commands(), ["look"])

    def test_doorway_adds_go(self) -> None:
        room = _room(doorways=[Doorway(direction="north", to="elsewhere")])
        self.assertEqual(room.get_commands(), ["look", "go"])

    def test_item_adds_its_commands(self) -> None:
        room = _room(items=[Item(id="sword", name="a sword")])
        self.assertEqual(room.get_commands(), ["look", "pick up"])

    def test_npc_adds_its_commands(self) -> None:
        room = _room(npcs=[NPC(id="old-tom", name="Old Tom", commands=["trade"])])
        self.assertEqual(room.get_commands(), ["look", "talk", "trade"])

    def test_player_adds_its_commands(self) -> None:
        room = _room(players=[Player(name="alice", current_room_id="room")])
        # Player.get_commands() is empty for now, so this is a no-op --
        # but it proves the room actually queries the player for them.
        self.assertEqual(room.get_commands(), ["look"])

    def test_multiple_items_dedupe_shared_commands(self) -> None:
        room = _room(
            items=[
                Item(id="sword", name="a sword"),
                Item(id="shield", name="a shield"),
            ]
        )
        self.assertEqual(room.get_commands(), ["look", "pick up"])

    def test_everything_combined(self) -> None:
        room = _room(
            doorways=[Doorway(direction="north", to="elsewhere")],
            items=[Item(id="sword", name="a sword")],
            npcs=[NPC(id="old-tom", name="Old Tom")],
            players=[Player(name="alice", current_room_id="room")],
        )
        self.assertEqual(room.get_commands(), ["look", "go", "pick up", "talk"])

    def test_json_configured_commands_are_added(self) -> None:
        room = _room(commands=["pray", "ring bell"])
        self.assertEqual(room.get_commands(), ["look", "pray", "ring bell"])

    def test_json_configured_commands_dedupe_against_defaults(self) -> None:
        room = _room(
            doorways=[Doorway(direction="north", to="elsewhere")],
            commands=["go", "pray"],
        )
        self.assertEqual(room.get_commands(), ["look", "go", "pray"])


class RoomFormatTests(unittest.TestCase):
    def test_no_npcs_section_when_room_has_none(self) -> None:
        self.assertNotIn("NPCs here:", _room().format())

    def test_npcs_are_listed_by_name(self) -> None:
        room = _room(npcs=[NPC(id="old-tom", name="Old Tom")])
        lines = room.format().split("\r\n")
        self.assertIn("NPCs here:", lines)
        self.assertIn("  - Old Tom", lines)


class ItemToDictTests(unittest.TestCase):
    def test_fields(self) -> None:
        item = Item(id="sword", name="a sword", description="Rusty.")
        self.assertEqual(
            item.to_dict(),
            {"id": "sword", "name": "a sword", "description": "Rusty."},
        )


class DoorwayToDictTests(unittest.TestCase):
    def test_fields(self) -> None:
        doorway = Doorway(direction="north", to="elsewhere", description="Narrow.")
        self.assertEqual(
            doorway.to_dict(),
            {"direction": "north", "to": "elsewhere", "description": "Narrow."},
        )


class NPCToDictTests(unittest.TestCase):
    def test_fields_including_nested_inventory(self) -> None:
        npc = NPC(
            id="old-tom",
            name="Old Tom",
            description="A bartender.",
            hostile=True,
            commands=["trade"],
            inventory=[Item(id="rag", name="a rag")],
        )
        self.assertEqual(
            npc.to_dict(),
            {
                "id": "old-tom",
                "name": "Old Tom",
                "description": "A bartender.",
                "hostile": True,
                "commands": ["trade"],
                "inventory": [{"id": "rag", "name": "a rag", "description": ""}],
            },
        )


class PlayerToDictTests(unittest.TestCase):
    def test_fields_including_nested_inventory(self) -> None:
        player = Player(
            name="alice", current_room_id="room", inventory=[Item(id="rock", name="a rock")]
        )
        result = player.to_dict()
        self.assertIsNone(result["id"])  # never persisted in this test
        self.assertEqual(result["name"], "alice")
        self.assertEqual(result["inventory"], [{"id": "rock", "name": "a rock", "description": ""}])

    def test_id_is_stringified_when_present(self) -> None:
        player_id = uuid.uuid4()
        player = Player(name="alice", current_room_id="room", id=player_id)
        self.assertEqual(player.to_dict()["id"], str(player_id))


class RoomToDictTests(unittest.TestCase):
    def test_empty_room(self) -> None:
        self.assertEqual(
            _room(description="A room.").to_dict(),
            {
                "id": "room",
                "structure_id": "structure",
                "description": "A room.",
                "items": [],
                "npcs": [],
                "players": [],
                "doorways": [],
                "commands": ["look"],
            },
        )

    def test_nested_collections_use_each_element_s_to_dict(self) -> None:
        room = _room(
            items=[Item(id="sword", name="a sword")],
            npcs=[NPC(id="old-tom", name="Old Tom")],
            players=[Player(name="alice", current_room_id="room")],
            doorways=[Doorway(direction="north", to="elsewhere")],
        )

        result = room.to_dict()

        self.assertEqual(result["items"], [Item(id="sword", name="a sword").to_dict()])
        self.assertEqual(result["npcs"], [NPC(id="old-tom", name="Old Tom").to_dict()])
        self.assertEqual(
            result["players"], [Player(name="alice", current_room_id="room").to_dict()]
        )
        self.assertEqual(
            result["doorways"], [Doorway(direction="north", to="elsewhere").to_dict()]
        )

    def test_commands_field_is_get_commands(self) -> None:
        room = _room(doorways=[Doorway(direction="north", to="elsewhere")])
        self.assertEqual(room.to_dict()["commands"], room.get_commands())


if __name__ == "__main__":
    unittest.main()
