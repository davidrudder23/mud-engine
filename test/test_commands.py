"""Tests for the Command implementations (look/go/quit), the
matches_verb helper, and CommandRunner's dispatch."""

from __future__ import annotations

import unittest

from mud.commands.command import Parameter, command_argument, matches_verb
from mud.commands.drop import DropCommand
from mud.commands.go import GoCommand
from mud.commands.help import HelpCommand
from mud.commands.look import LookCommand
from mud.commands.order import OrderCommand
from mud.commands.pick_up import PickUpCommand
from mud.commands.quit import QuitCommand
from mud.commands.runner import CommandRunner
from mud.item import Item
from mud.player import Player
from mud.room import Room
from mud.session import SessionRegistry

from test.fakes import FakePlayerRepository
from test.helpers import build_two_room_world


def _expected_look(room: Room) -> str:
    """What LookCommand should return for `room`: its description plus
    the "you can try" hint built from Room.get_commands()."""
    hint = f"You can try: {', '.join(room.get_commands())}"
    return room.format() + "\r\n" + hint


class MatchesVerbTests(unittest.TestCase):
    def test_matches_bare_verb(self) -> None:
        self.assertTrue(matches_verb("look", "look"))

    def test_matches_verb_with_trailing_argument(self) -> None:
        self.assertTrue(matches_verb("go north", "go"))

    def test_case_insensitive(self) -> None:
        self.assertTrue(matches_verb("LOOK around", "look"))
        self.assertTrue(matches_verb("Go North", "go"))

    def test_no_match_for_unrelated_line(self) -> None:
        self.assertFalse(matches_verb("quit", "go"))

    def test_prefix_only_not_word_boundary(self) -> None:
        # Documented, intentional quirk (see command.py): this is a
        # plain prefix check, so "goblin" matches verb "go".
        self.assertTrue(matches_verb("goblin", "go"))


class CommandArgumentTests(unittest.TestCase):
    def test_returns_text_after_verb(self) -> None:
        self.assertEqual(command_argument("pick up sword", "pick up"), "sword")

    def test_multi_word_verb_not_broken_by_whitespace_split(self) -> None:
        # A plain `line.split(maxsplit=1)` (what `go` uses) would cut
        # after "pick", not after "pick up" -- command_argument must not.
        self.assertEqual(command_argument("pick up a rusty sword", "pick up"), "a rusty sword")

    def test_no_argument_returns_empty_string(self) -> None:
        self.assertEqual(command_argument("pick up", "pick up"), "")
        self.assertEqual(command_argument("pick up   ", "pick up"), "")

    def test_case_of_verb_in_line_does_not_matter(self) -> None:
        self.assertEqual(command_argument("PICK UP sword", "pick up"), "sword")


class CommandTestCase(unittest.TestCase):
    """Common fixture: a two-room world, a session registry, and a fake
    player repository, for the command tests below."""

    def setUp(self) -> None:
        self.world, self.start_id, self.north_id = build_two_room_world()
        self.sessions = SessionRegistry()
        self.repo = FakePlayerRepository()

        self.player = self.repo.create("alice", starting_room_id=self.start_id)
        self.world.place_player(self.player)
        self.session = self.sessions.create(self.player)


class LookCommandTests(CommandTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.command = LookCommand(self.sessions, self.world)

    def test_matches_look_and_only_look(self) -> None:
        self.assertTrue(self.command.match_command("look"))
        self.assertTrue(self.command.match_command("look around"))
        self.assertFalse(self.command.match_command("go north"))

    def test_takes_no_parameters(self) -> None:
        self.assertEqual(self.command.parameters(), [])

    def test_run_returns_current_room_description(self) -> None:
        result = self.command.run_command("look", self.session.id)
        self.assertEqual(result, _expected_look(self.world.get_room(self.start_id)))

    def test_run_reflects_room_after_move(self) -> None:
        self.world.move_player(self.player, self.north_id)
        result = self.command.run_command("look", self.session.id)
        self.assertEqual(result, _expected_look(self.world.get_room(self.north_id)))

    def test_run_appends_you_can_try_hint(self) -> None:
        result = self.command.run_command("look", self.session.id)
        self.assertIn("You can try: look, go", result)


class GoCommandTests(CommandTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.command = GoCommand(self.sessions, self.world, self.repo)

    def test_takes_one_required_string_parameter(self) -> None:
        self.assertEqual(
            self.command.parameters(),
            [Parameter(name="direction", type=str, required=True)],
        )

    def test_no_direction_given(self) -> None:
        self.assertEqual(self.command.run_command("go", self.session.id), "Go where?")
        # Nothing should have moved.
        self.assertEqual(self.player.current_room_id, self.start_id)

    def test_unknown_direction(self) -> None:
        result = self.command.run_command("go west", self.session.id)
        self.assertEqual(result, "You can't go 'west' from here.")
        self.assertEqual(self.player.current_room_id, self.start_id)

    def test_direction_match_is_case_sensitive(self) -> None:
        # Doorway direction is "north"; "North" must not match.
        result = self.command.run_command("go North", self.session.id)
        self.assertEqual(result, "You can't go 'North' from here.")

    def test_valid_move_updates_world_and_persists(self) -> None:
        result = self.command.run_command("go north", self.session.id)

        self.assertEqual(self.player.current_room_id, self.north_id)
        self.assertIn(self.player, self.world.get_room(self.north_id).players)
        self.assertNotIn(self.player, self.world.get_room(self.start_id).players)
        self.assertEqual(result, self.world.get_room(self.north_id).format())

        # The move must survive a reconnect, i.e. it was actually saved.
        persisted = self.repo.get_by_id(self.player.id)
        self.assertEqual(persisted.current_room_id, self.north_id)

    def test_round_trip_move(self) -> None:
        self.command.run_command("go north", self.session.id)
        self.command.run_command("go south", self.session.id)
        self.assertEqual(self.player.current_room_id, self.start_id)
        self.assertIn(self.player, self.world.get_room(self.start_id).players)


class HelpCommandTests(CommandTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.look = LookCommand(self.sessions, self.world)
        self.go = GoCommand(self.sessions, self.world, self.repo)
        self.quit = QuitCommand(self.sessions)
        self.command = HelpCommand(
            self.sessions, self.world, [self.look, self.go, self.quit]
        )

    def test_takes_no_parameters(self) -> None:
        self.assertEqual(self.command.parameters(), [])

    def test_matches_help(self) -> None:
        self.assertTrue(self.command.match_command("help"))
        self.assertFalse(self.command.match_command("look"))

    def test_lists_describe_of_every_command_that_matches_the_room(self) -> None:
        result = self.command.run_command("help", self.session.id)
        expected = "\r\n".join(
            command.describe() for command in (self.look, self.go, self.quit)
        )
        self.assertEqual(result, expected)

    def test_lists_itself_when_included_in_its_own_command_list(self) -> None:
        commands = [self.look, self.go, self.quit]
        command = HelpCommand(self.sessions, self.world, commands)
        commands.append(command)

        result = command.run_command("help", self.session.id)
        self.assertIn(command.describe(), result.splitlines())

    def test_room_match_is_a_stub_that_always_returns_true(self) -> None:
        room = self.world.get_room(self.start_id)
        self.assertTrue(self.look.match_room(room))
        self.assertTrue(self.go.match_room(room))
        self.assertTrue(self.quit.match_room(room))
        self.assertTrue(self.command.match_room(room))

    def test_lists_item_commands_that_have_no_registered_command(self) -> None:
        # "pick up" comes from Room.get_commands() via the item, not from
        # any Command in self._commands -- help must still surface it.
        self.world.get_room(self.start_id).items.append(Item(id="rock", name="a rock"))

        result = self.command.run_command("help", self.session.id)

        self.assertIn("pick up", result.splitlines())

    def test_lists_json_configured_room_commands(self) -> None:
        self.world.get_room(self.start_id).commands.append("ring bell")

        result = self.command.run_command("help", self.session.id)

        self.assertIn("ring bell", result.splitlines())

    def test_does_not_duplicate_a_verb_already_covered_by_a_registered_command(
        self,
    ) -> None:
        # The room's own "look"/"go" (from Room.get_commands()) must not
        # be listed a second time alongside LookCommand/GoCommand's
        # describe() text.
        result = self.command.run_command("help", self.session.id)
        lines = result.splitlines()
        self.assertEqual(lines.count("look"), 0)
        self.assertEqual(lines.count("go"), 0)


class QuitCommandTests(CommandTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.command = QuitCommand(self.sessions)

    def test_matches_quit(self) -> None:
        self.assertTrue(self.command.match_command("quit"))
        self.assertFalse(self.command.match_command("look"))

    def test_takes_no_parameters(self) -> None:
        self.assertEqual(self.command.parameters(), [])

    def test_run_sets_should_close_and_returns_empty(self) -> None:
        self.assertFalse(self.session.should_close)
        result = self.command.run_command("quit", self.session.id)
        self.assertEqual(result, "")
        self.assertTrue(self.session.should_close)


class PickUpCommandTests(CommandTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.command = PickUpCommand(self.sessions, self.world)
        self.room = self.world.get_room(self.start_id)

    def test_takes_one_required_string_parameter(self) -> None:
        self.assertEqual(
            self.command.parameters(), [Parameter(name="item", type=str, required=True)]
        )

    def test_matches_pick_up_and_only_pick_up(self) -> None:
        self.assertTrue(self.command.match_command("pick up sword"))
        self.assertFalse(self.command.match_command("look"))
        self.assertFalse(self.command.match_command("pick"))

    def test_no_item_given(self) -> None:
        result = self.command.run_command("pick up", self.session.id)
        self.assertEqual(result, "Pick up what?")

    def test_item_not_present(self) -> None:
        result = self.command.run_command("pick up sword", self.session.id)
        self.assertEqual(result, "You don't see 'sword' here.")

    def test_matches_item_by_id(self) -> None:
        item = Item(id="rock", name="A Shiny Rock")
        self.room.items.append(item)

        result = self.command.run_command("pick up rock", self.session.id)

        self.assertEqual(result, "You pick up A Shiny Rock.")

    def test_matches_item_by_name_substring_case_insensitive(self) -> None:
        item = Item(id="rock", name="A Shiny Rock")
        self.room.items.append(item)

        result = self.command.run_command("pick up SHINY", self.session.id)

        self.assertEqual(result, "You pick up A Shiny Rock.")

    def test_moves_item_from_room_to_player_inventory(self) -> None:
        item = Item(id="rock", name="A Shiny Rock")
        self.room.items.append(item)

        self.command.run_command("pick up rock", self.session.id)

        self.assertNotIn(item, self.room.items)
        self.assertIn(item, self.player.inventory)


class DropCommandTests(CommandTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.command = DropCommand(self.sessions, self.world)
        self.room = self.world.get_room(self.start_id)

    def test_takes_one_required_string_parameter(self) -> None:
        self.assertEqual(
            self.command.parameters(), [Parameter(name="item", type=str, required=True)]
        )

    def test_matches_drop_and_only_drop(self) -> None:
        self.assertTrue(self.command.match_command("drop sword"))
        self.assertFalse(self.command.match_command("look"))

    def test_no_item_given(self) -> None:
        result = self.command.run_command("drop", self.session.id)
        self.assertEqual(result, "Drop what?")

    def test_item_not_carried(self) -> None:
        result = self.command.run_command("drop sword", self.session.id)
        self.assertEqual(result, "You aren't carrying 'sword'.")

    def test_matches_item_by_id(self) -> None:
        item = Item(id="rock", name="A Shiny Rock")
        self.player.inventory.append(item)

        result = self.command.run_command("drop rock", self.session.id)

        self.assertEqual(result, "You drop A Shiny Rock.")

    def test_matches_item_by_name_substring_case_insensitive(self) -> None:
        item = Item(id="rock", name="A Shiny Rock")
        self.player.inventory.append(item)

        result = self.command.run_command("drop SHINY", self.session.id)

        self.assertEqual(result, "You drop A Shiny Rock.")

    def test_moves_item_from_player_inventory_to_room(self) -> None:
        item = Item(id="rock", name="A Shiny Rock")
        self.player.inventory.append(item)

        self.command.run_command("drop rock", self.session.id)

        self.assertNotIn(item, self.player.inventory)
        self.assertIn(item, self.room.items)

    def test_only_drops_from_the_players_current_room(self) -> None:
        # Item picked up in start_id, then the player moves before
        # dropping it -- it must land in the room they're standing in
        # now, not the one they picked it up from.
        item = Item(id="rock", name="A Shiny Rock")
        self.player.inventory.append(item)
        self.world.move_player(self.player, self.north_id)

        self.command.run_command("drop rock", self.session.id)

        self.assertNotIn(item, self.room.items)
        self.assertIn(item, self.world.get_room(self.north_id).items)

    def test_round_trip_pick_up_then_drop(self) -> None:
        item = Item(id="rock", name="A Shiny Rock")
        self.room.items.append(item)

        pick_up = PickUpCommand(self.sessions, self.world)
        pick_up.run_command("pick up rock", self.session.id)
        self.assertNotIn(item, self.room.items)
        self.assertIn(item, self.player.inventory)

        self.command.run_command("drop rock", self.session.id)
        self.assertIn(item, self.room.items)
        self.assertNotIn(item, self.player.inventory)


class OrderCommandTests(CommandTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.command = OrderCommand()

    def test_takes_one_required_string_parameter(self) -> None:
        self.assertEqual(
            self.command.parameters(), [Parameter(name="drink", type=str, required=True)]
        )

    def test_matches_order_and_only_order(self) -> None:
        self.assertTrue(self.command.match_command("order drink"))
        self.assertFalse(self.command.match_command("go north"))

    def test_no_item_given(self) -> None:
        result = self.command.run_command("order", self.session.id)
        self.assertEqual(result, "Order what?")

    def test_orders_whatever_was_asked_for(self) -> None:
        result = self.command.run_command("order drink", self.session.id)
        self.assertIn("drink", result)


class CommandRunnerTests(CommandTestCase):
    def setUp(self) -> None:
        super().setUp()
        commands = [
            LookCommand(self.sessions, self.world),
            GoCommand(self.sessions, self.world, self.repo),
            QuitCommand(self.sessions),
            PickUpCommand(self.sessions, self.world),
            DropCommand(self.sessions, self.world),
            OrderCommand(),
        ]
        commands.append(HelpCommand(self.sessions, self.world, commands))
        self.runner = CommandRunner(commands)

    def test_dispatches_to_help(self) -> None:
        result = self.runner.run("help", self.session.id)
        self.assertIn("look", result)
        self.assertIn("go <dir>", result)
        self.assertIn("quit", result)
        self.assertIn("help", result)
        self.assertIn("pick up <item>", result)
        self.assertIn("drop <item>", result)
        self.assertIn("order <drink>", result)

    def test_dispatches_to_pick_up(self) -> None:
        room = self.world.get_room(self.start_id)
        room.items.append(Item(id="rock", name="a rock"))

        result = self.runner.run("pick up rock", self.session.id)

        self.assertEqual(result, "You pick up a rock.")

    def test_dispatches_to_drop(self) -> None:
        self.player.inventory.append(Item(id="rock", name="a rock"))

        result = self.runner.run("drop rock", self.session.id)

        self.assertEqual(result, "You drop a rock.")

    def test_dispatches_to_order(self) -> None:
        result = self.runner.run("order drink", self.session.id)
        self.assertIn("drink", result)

    def test_dispatches_to_first_matching_command(self) -> None:
        result = self.runner.run("look", self.session.id)
        self.assertEqual(result, _expected_look(self.world.get_room(self.start_id)))

    def test_unmatched_line_returns_unknown_command(self) -> None:
        result = self.runner.run("dance", self.session.id)
        self.assertEqual(result, "Unknown command: 'dance'")

    def test_unmatched_line_strips_whitespace_in_message(self) -> None:
        result = self.runner.run("  dance  ", self.session.id)
        self.assertEqual(result, "Unknown command: 'dance'")


if __name__ == "__main__":
    unittest.main()
