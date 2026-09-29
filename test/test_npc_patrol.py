"""Tests for mud.npc_patrol: wander_once (the synchronous move logic)
and run_forever (the per-NPC background loop around it)."""

from __future__ import annotations

import asyncio
import unittest
import unittest.mock

from mud.doorway import Doorway
from mud.npc import WanderingNPC
from mud.npc_patrol import run_forever, wander_once
from mud.room import Room
from mud.structure import Structure
from mud.world import World

from test.helpers import build_two_room_world


class WanderOnceTests(unittest.TestCase):
    def setUp(self) -> None:
        self.world, self.start_id, self.north_id = build_two_room_world()
        self.npc = WanderingNPC(id="watchman", name="A Watchman", room_id=self.start_id)
        self.world.get_room(self.start_id).npcs.append(self.npc)

    def test_moves_through_a_doorway(self) -> None:
        # build_two_room_world's "start" has exactly one doorway (north),
        # so this is deterministic without needing to fake the rng.
        wander_once(self.npc, self.world)

        self.assertEqual(self.npc.room_id, self.north_id)
        self.assertNotIn(self.npc, self.world.get_room(self.start_id).npcs)
        self.assertIn(self.npc, self.world.get_room(self.north_id).npcs)

    def test_no_doorways_is_a_no_op(self) -> None:
        world = World()
        structure = Structure(id="s", name="S")
        world.structures[structure.id] = structure
        room = Room(id="dead-end", structure_id=structure.id)  # no doorways
        world.rooms[room.id] = room
        world.place_room(room)

        npc = WanderingNPC(id="watchman", name="A Watchman", room_id=room.id)
        room.npcs.append(npc)

        wander_once(npc, world)

        self.assertEqual(npc.room_id, room.id)
        self.assertIn(npc, room.npcs)

    def test_uses_the_given_rng_to_choose_among_multiple_doorways(self) -> None:
        # A room with several doorways -- pin the "random" choice via an
        # injected, seeded Random so the destination is deterministic.
        world = World()
        structure = Structure(id="s", name="S")
        world.structures[structure.id] = structure
        hub = Room(id="hub", structure_id=structure.id)
        for direction, to in [("north", "a"), ("east", "b"), ("south", "c")]:
            hub.doorways.append(Doorway(direction=direction, to=to))
        for room_id in ["a", "b", "c"]:
            room = Room(id=room_id, structure_id=structure.id)
            world.rooms[room.id] = room
            world.place_room(room)
        world.rooms[hub.id] = hub
        world.place_room(hub)

        npc = WanderingNPC(id="watchman", name="A Watchman", room_id="hub")
        hub.npcs.append(npc)

        class _FixedChoice:
            def choice(self, seq):
                return seq[1]  # always "east" -> "b"

        wander_once(npc, world, rng=_FixedChoice())

        self.assertEqual(npc.room_id, "b")


class RunForeverTests(unittest.IsolatedAsyncioTestCase):
    async def test_moves_repeatedly_on_its_own_interval(self) -> None:
        world, start_id, _ = build_two_room_world()
        npc = WanderingNPC(
            id="watchman", name="A Watchman", room_id=start_id, move_interval=0
        )
        world.get_room(start_id).npcs.append(npc)

        calls = 0

        async def _counting_sleep(_seconds: float) -> None:
            nonlocal calls
            calls += 1
            if calls >= 5:
                raise asyncio.CancelledError  # stop the loop deterministically
            await asyncio.sleep(0)  # yield, but don't actually wait

        with unittest.mock.patch("mud.npc_patrol.asyncio.sleep", _counting_sleep):
            with self.assertRaises(asyncio.CancelledError):
                await run_forever(npc, world)

        # One wander_once per sleep -- proves the loop actually repeats,
        # not just moves once and stops.
        self.assertEqual(calls, 5)

    async def test_cancellation_stops_it_cleanly(self) -> None:
        world, start_id, _ = build_two_room_world()
        npc = WanderingNPC(
            id="watchman", name="A Watchman", room_id=start_id, move_interval=0
        )
        world.get_room(start_id).npcs.append(npc)

        task = asyncio.create_task(run_forever(npc, world))
        await asyncio.sleep(0)
        task.cancel()

        with self.assertRaises(asyncio.CancelledError):
            await task
        self.assertTrue(task.cancelled())


if __name__ == "__main__":
    unittest.main()
