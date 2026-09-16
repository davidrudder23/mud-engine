"""Tests for mud.world.World: room/structure lookup and the
player/room/structure bookkeeping helpers that keep both sides of each
relationship (room<->structure, player<->room) in sync."""

from __future__ import annotations

import unittest

from mud.player import Player
from mud.structure import Structure
from mud.world import World

from test.helpers import build_two_room_world


class LookupTests(unittest.TestCase):
    def test_get_room_missing_raises_keyerror(self) -> None:
        world = World()
        with self.assertRaises(KeyError):
            world.get_room("nowhere")

    def test_get_structure_missing_raises_keyerror(self) -> None:
        world = World()
        with self.assertRaises(KeyError):
            world.get_structure("nowhere")

    def test_len_and_iter_reflect_rooms(self) -> None:
        world, start_id, north_id = build_two_room_world()
        self.assertEqual(len(world), 2)
        self.assertCountEqual([r.id for r in world], [start_id, north_id])


class PlaceRoomTests(unittest.TestCase):
    def test_place_room_is_idempotent(self) -> None:
        world, start_id, _ = build_two_room_world()
        structure = world.get_structure("test-structure")
        room = world.get_room(start_id)

        before = list(structure.rooms)
        world.place_room(room)  # already placed by the helper
        self.assertEqual(structure.rooms, before)


class MoveRoomToStructureTests(unittest.TestCase):
    def setUp(self) -> None:
        self.world, self.start_id, self.north_id = build_two_room_world()
        self.other = Structure(id="other-structure", name="Other")
        self.world.structures[self.other.id] = self.other

    def test_moves_room_between_structures(self) -> None:
        room = self.world.get_room(self.start_id)
        original = self.world.get_structure("test-structure")

        self.world.move_room_to_structure(room, self.other.id)

        self.assertEqual(room.structure_id, self.other.id)
        self.assertIn(room, self.other.rooms)
        self.assertNotIn(room, original.rooms)

    def test_unknown_destination_raises_and_does_not_mutate(self) -> None:
        room = self.world.get_room(self.start_id)
        original = self.world.get_structure("test-structure")

        with self.assertRaises(KeyError):
            self.world.move_room_to_structure(room, "ghost-structure")

        # Destination is validated before anything is touched.
        self.assertEqual(room.structure_id, "test-structure")
        self.assertIn(room, original.rooms)


class PlacePlayerTests(unittest.TestCase):
    def test_place_player_adds_to_room_occupants(self) -> None:
        world, start_id, _ = build_two_room_world()
        player = Player(name="alice", current_room_id=start_id)

        world.place_player(player)

        self.assertIn(player, world.get_room(start_id).players)

    def test_place_player_is_idempotent_for_same_instance(self) -> None:
        world, start_id, _ = build_two_room_world()
        player = Player(name="alice", current_room_id=start_id)

        world.place_player(player)
        world.place_player(player)

        self.assertEqual(world.get_room(start_id).players.count(player), 1)

    def test_place_player_unknown_room_raises(self) -> None:
        world, _, _ = build_two_room_world()
        player = Player(name="alice", current_room_id="nowhere")
        with self.assertRaises(KeyError):
            world.place_player(player)

    def test_two_players_same_name_are_independent(self) -> None:
        # Mirrors the real scenario: two logins under the same name are
        # two distinct Player instances (Player uses eq=False for
        # exactly this reason -- see mud/player.py).
        world, start_id, _ = build_two_room_world()
        alice_1 = Player(name="alice", current_room_id=start_id)
        alice_2 = Player(name="alice", current_room_id=start_id)

        world.place_player(alice_1)
        world.place_player(alice_2)

        occupants = world.get_room(start_id).players
        self.assertEqual(len(occupants), 2)
        self.assertIn(alice_1, occupants)
        self.assertIn(alice_2, occupants)


class MovePlayerTests(unittest.TestCase):
    def test_move_player_updates_both_rooms_and_current_room_id(self) -> None:
        world, start_id, north_id = build_two_room_world()
        player = Player(name="alice", current_room_id=start_id)
        world.place_player(player)

        world.move_player(player, north_id)

        self.assertEqual(player.current_room_id, north_id)
        self.assertNotIn(player, world.get_room(start_id).players)
        self.assertIn(player, world.get_room(north_id).players)

    def test_move_player_unknown_destination_raises_and_does_not_mutate(self) -> None:
        world, start_id, _ = build_two_room_world()
        player = Player(name="alice", current_room_id=start_id)
        world.place_player(player)

        with self.assertRaises(KeyError):
            world.move_player(player, "nowhere")

        self.assertEqual(player.current_room_id, start_id)
        self.assertIn(player, world.get_room(start_id).players)

    def test_move_player_not_previously_placed_still_lands_in_new_room(self) -> None:
        world, start_id, north_id = build_two_room_world()
        player = Player(name="alice", current_room_id=start_id)  # never placed

        world.move_player(player, north_id)

        self.assertEqual(player.current_room_id, north_id)
        self.assertIn(player, world.get_room(north_id).players)


class RemovePlayerTests(unittest.TestCase):
    def test_remove_player_clears_occupancy_but_keeps_current_room_id(self) -> None:
        world, start_id, _ = build_two_room_world()
        player = Player(name="alice", current_room_id=start_id)
        world.place_player(player)

        world.remove_player(player)

        self.assertNotIn(player, world.get_room(start_id).players)
        self.assertEqual(player.current_room_id, start_id)  # unchanged

    def test_remove_player_not_present_is_a_no_op(self) -> None:
        world, start_id, _ = build_two_room_world()
        player = Player(name="alice", current_room_id=start_id)  # never placed
        world.remove_player(player)  # must not raise


if __name__ == "__main__":
    unittest.main()
