"""Tests for Room.get_commands() (and the Item/Player get_commands() it
delegates to): the "you can try" verbs look/help build their listings
from."""

from __future__ import annotations

import unittest

from mud.doorway import Doorway
from mud.item import Item
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


class RoomCommandsTests(unittest.TestCase):
    def test_empty_room_only_offers_look(self) -> None:
        self.assertEqual(_room().get_commands(), ["look"])

    def test_doorway_adds_go(self) -> None:
        room = _room(doorways=[Doorway(direction="north", to="elsewhere")])
        self.assertEqual(room.get_commands(), ["look", "go"])

    def test_item_adds_its_commands(self) -> None:
        room = _room(items=[Item(id="sword", name="a sword")])
        self.assertEqual(room.get_commands(), ["look", "pick up"])

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
            players=[Player(name="alice", current_room_id="room")],
        )
        self.assertEqual(room.get_commands(), ["look", "go", "pick up"])

    def test_json_configured_commands_are_added(self) -> None:
        room = _room(commands=["pray", "ring bell"])
        self.assertEqual(room.get_commands(), ["look", "pray", "ring bell"])

    def test_json_configured_commands_dedupe_against_defaults(self) -> None:
        room = _room(
            doorways=[Doorway(direction="north", to="elsewhere")],
            commands=["go", "pray"],
        )
        self.assertEqual(room.get_commands(), ["look", "go", "pray"])


if __name__ == "__main__":
    unittest.main()
