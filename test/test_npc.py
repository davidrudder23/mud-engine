"""Tests for NPC: defaults, format(), get_commands(), equality,
spawn() (definition -> independent room instance), and WanderingNPC
(the subclass that adds move_interval)."""

from __future__ import annotations

import unittest

from mud.item import Item
from mud.npc import NPC, WanderingNPC


class NPCDefaultsTests(unittest.TestCase):
    def test_only_id_and_name_are_required(self) -> None:
        npc = NPC(id="old-tom", name="Old Tom")
        self.assertEqual(npc.id, "old-tom")
        self.assertEqual(npc.name, "Old Tom")
        self.assertEqual(npc.description, "")
        self.assertFalse(npc.hostile)
        self.assertEqual(npc.commands, [])
        self.assertEqual(npc.inventory, [])
        self.assertIsNone(npc.room_id)  # a definition isn't standing anywhere

    def test_two_npcs_get_independent_lists(self) -> None:
        # A mutable default field (list) shared across instances would
        # be a classic bug -- dataclass's default_factory avoids it, but
        # this pins the behavior.
        a = NPC(id="a", name="a")
        b = NPC(id="b", name="b")
        a.commands.append("trade")
        a.inventory.append(Item(id="key", name="a key"))
        self.assertEqual(b.commands, [])
        self.assertEqual(b.inventory, [])


class NPCEqualityTests(unittest.TestCase):
    def test_identical_fields_are_not_equal(self) -> None:
        # eq=False (see npc.py): two independently spawned instances of
        # the same definition must stay distinct even when every field
        # matches, the same way two Player logins under one name do --
        # otherwise a room.npcs.remove(npc)/`in` check could silently
        # act on the wrong one.
        a = NPC(id="old-tom", name="Old Tom")
        b = NPC(id="old-tom", name="Old Tom")
        self.assertNotEqual(a, b)
        self.assertEqual(a, a)


class NPCSpawnTests(unittest.TestCase):
    def test_spawn_copies_static_fields(self) -> None:
        definition = NPC(
            id="old-tom", name="Old Tom", description="A bartender.",
            hostile=True, commands=["trade"],
        )
        instance = definition.spawn("taproom")
        self.assertEqual(instance.id, "old-tom")
        self.assertEqual(instance.name, "Old Tom")
        self.assertEqual(instance.description, "A bartender.")
        self.assertTrue(instance.hostile)
        self.assertEqual(instance.commands, ["trade"])

    def test_spawn_sets_room_id(self) -> None:
        definition = NPC(id="old-tom", name="Old Tom")
        self.assertIsNone(definition.room_id)

        instance = definition.spawn("taproom")

        self.assertEqual(instance.room_id, "taproom")
        self.assertIsNone(definition.room_id)  # the definition is untouched

    def test_spawn_returns_a_distinct_instance(self) -> None:
        definition = NPC(id="old-tom", name="Old Tom")
        instance = definition.spawn("taproom")
        self.assertIsNot(instance, definition)
        self.assertNotEqual(instance, definition)  # eq=False

    def test_two_spawns_in_different_rooms_get_their_own_room_id(self) -> None:
        definition = NPC(id="shopkeeper", name="Shopkeeper")
        a = definition.spawn("room-a")
        b = definition.spawn("room-b")
        self.assertIsNot(a, b)
        self.assertEqual(a.room_id, "room-a")
        self.assertEqual(b.room_id, "room-b")

    def test_two_spawns_are_independent_of_each_other(self) -> None:
        definition = NPC(id="shopkeeper", name="Shopkeeper")
        a = definition.spawn("room")
        b = definition.spawn("room")
        self.assertIsNot(a, b)

        a.commands.append("haggle")
        a.inventory.append(Item(id="rag", name="a rag"))

        self.assertEqual(b.commands, [])
        self.assertEqual(b.inventory, [])

    def test_spawn_copies_inventory_items_independently(self) -> None:
        definition = NPC(
            id="old-tom", name="Old Tom", inventory=[Item(id="rag", name="a rag")]
        )
        instance = definition.spawn("taproom")

        self.assertEqual(len(instance.inventory), 1)
        self.assertIsNot(instance.inventory[0], definition.inventory[0])
        self.assertEqual(instance.inventory[0].name, "a rag")

        # Mutating the spawned instance's inventory doesn't touch the
        # definition -- otherwise the next spawn() would copy the leak.
        instance.inventory.clear()
        self.assertEqual(len(definition.inventory), 1)

    def test_spawning_a_spawn_works(self) -> None:
        definition = NPC(id="old-tom", name="Old Tom", commands=["trade"])
        instance = definition.spawn("room-a")
        instance.inventory.append(Item(id="rag", name="a rag"))

        respawn = instance.spawn("room-b")

        self.assertIsNot(respawn, instance)
        self.assertEqual(respawn.commands, ["trade"])
        self.assertEqual(respawn.room_id, "room-b")
        self.assertEqual(instance.room_id, "room-a")  # unchanged by respawning
        self.assertEqual(len(respawn.inventory), 1)
        self.assertIsNot(respawn.inventory[0], instance.inventory[0])


class WanderingNPCTests(unittest.TestCase):
    def test_default_move_interval(self) -> None:
        self.assertEqual(WanderingNPC(id="watchman", name="A Watchman").move_interval, 120.0)

    def test_move_interval_is_configurable(self) -> None:
        npc = WanderingNPC(id="watchman", name="A Watchman", move_interval=30)
        self.assertEqual(npc.move_interval, 30)

    def test_is_still_an_npc(self) -> None:
        # Same get_commands()/format()/etc as a plain NPC -- WanderingNPC
        # only adds behavior (via npc_patrol.py) and one field.
        npc = WanderingNPC(id="watchman", name="A Watchman")
        self.assertIsInstance(npc, NPC)
        self.assertEqual(npc.get_commands(), ["talk"])
        self.assertEqual(npc.format(), "A Watchman")

    def test_spawn_returns_a_wandering_npc_not_a_plain_npc(self) -> None:
        # NPC.spawn() uses type(self), not a hardcoded NPC(...), so a
        # subclass's spawn() stays that subclass -- see npc.py.
        definition = WanderingNPC(id="watchman", name="A Watchman", move_interval=45)
        instance = definition.spawn("muster-room")
        self.assertIsInstance(instance, WanderingNPC)
        self.assertEqual(instance.move_interval, 45)
        self.assertEqual(instance.room_id, "muster-room")

    def test_spawn_of_a_spawn_keeps_its_own_move_interval(self) -> None:
        definition = WanderingNPC(id="watchman", name="A Watchman", move_interval=45)
        instance = definition.spawn("room-a")
        respawn = instance.spawn("room-b")
        self.assertEqual(respawn.move_interval, 45)

    def test_to_dict_includes_move_interval(self) -> None:
        npc = WanderingNPC(id="watchman", name="A Watchman", move_interval=45)
        self.assertEqual(npc.to_dict()["move_interval"], 45)
        # Still has everything a plain NPC's to_dict() has too.
        self.assertEqual(npc.to_dict()["id"], "watchman")


class NPCFormatTests(unittest.TestCase):
    def test_format_is_the_name(self) -> None:
        npc = NPC(id="old-tom", name="Old Tom", description="A grizzled bartender.")
        self.assertEqual(npc.format(), "Old Tom")


class NPCCommandsTests(unittest.TestCase):
    def test_always_offers_talk(self) -> None:
        self.assertEqual(NPC(id="old-tom", name="Old Tom").get_commands(), ["talk"])

    def test_authored_commands_are_appended_in_order(self) -> None:
        npc = NPC(id="old-tom", name="Old Tom", commands=["trade", "follow"])
        self.assertEqual(npc.get_commands(), ["talk", "trade", "follow"])

    def test_deduplicates_repeated_commands(self) -> None:
        npc = NPC(id="old-tom", name="Old Tom", commands=["talk", "trade", "talk"])
        self.assertEqual(npc.get_commands(), ["talk", "trade"])

    def test_get_commands_does_not_consider_inventory(self) -> None:
        # Deliberate for now (see npc.py) -- an inventory item's own
        # get_commands() (e.g. Item's "pick up") isn't offered just
        # because an NPC happens to be carrying it.
        npc = NPC(
            id="old-tom", name="Old Tom", inventory=[Item(id="sword", name="a sword")]
        )
        self.assertEqual(npc.get_commands(), ["talk"])


if __name__ == "__main__":
    unittest.main()
