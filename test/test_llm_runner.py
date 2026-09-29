"""Tests for LLMCommandRunner: matching a line to a command (and
extracting its parameters) via an LLMClient, instead of CommandRunner's
matches_verb prefix check. Uses test.fakes.FakeLLMClient throughout --
no real network call, no API key needed.
"""

from __future__ import annotations

import json
import unittest

from mud.commands.go import GoCommand
from mud.commands.help import HelpCommand
from mud.commands.llm_runner import LLMCommandRunner, build_system_prompt
from mud.commands.look import LookCommand
from mud.commands.pick_up import PickUpCommand
from mud.commands.quit import QuitCommand
from mud.item import Item
from mud.llm.llm_client import LLMRequestError
from mud.session import SessionRegistry

from test.fakes import FakeLLMClient, FakePlayerRepository
from test.helpers import build_two_room_world


def _reply(command: str | None, **parameters: str) -> str:
    return json.dumps({"command": command, "parameters": parameters})


class LLMCommandRunnerTestCase(unittest.TestCase):
    """Common fixture: a two-room world, a session, and a runner wired
    up with look/go/quit/pick up -- enough variety to exercise
    no-parameter, required-parameter, and multi-word-verb commands."""

    def setUp(self) -> None:
        self.world, self.start_id, self.north_id = build_two_room_world()
        self.sessions = SessionRegistry()
        self.repo = FakePlayerRepository()

        self.player = self.repo.create("alice", starting_room_id=self.start_id)
        self.world.place_player(self.player)
        self.session = self.sessions.create(self.player)

        self.look = LookCommand(self.sessions, self.world)
        self.go = GoCommand(self.sessions, self.world, self.repo)
        self.quit = QuitCommand(self.sessions)
        self.pick_up = PickUpCommand(self.sessions, self.world)
        self.commands = [self.look, self.go, self.quit, self.pick_up]

        self.llm = FakeLLMClient()
        self.runner = LLMCommandRunner(self.llm, self.commands)


class BuildSystemPromptTests(unittest.TestCase):
    def setUp(self) -> None:
        self.world, self.start_id, _ = build_two_room_world()
        sessions = SessionRegistry()
        repo = FakePlayerRepository()
        self.commands = [
            LookCommand(sessions, self.world),
            GoCommand(sessions, self.world, repo),
        ]

    def test_includes_each_commands_name(self) -> None:
        prompt = build_system_prompt(self.commands)
        self.assertIn('"look"', prompt)
        self.assertIn('"go"', prompt)

    def test_includes_each_commands_parameters(self) -> None:
        prompt = build_system_prompt(self.commands)
        self.assertIn("none", prompt)  # look takes none
        self.assertIn("direction (str, required)", prompt)  # go's parameter

    def test_instructs_json_only_reply(self) -> None:
        prompt = build_system_prompt(self.commands)
        self.assertIn("JSON object", prompt)
        self.assertIn('"command"', prompt)
        self.assertIn('"parameters"', prompt)


class LLMCommandRunnerDispatchTests(LLMCommandRunnerTestCase):
    def test_sends_the_raw_line_as_the_prompt(self) -> None:
        self.llm.reply = _reply("look")
        self.runner.run("take a look around", self.session.id)
        self.assertEqual(self.llm.calls[0]["prompt"], "take a look around")

    def test_sends_a_system_prompt(self) -> None:
        self.llm.reply = _reply("look")
        self.runner.run("look", self.session.id)
        self.assertIsNotNone(self.llm.calls[0]["system"])

    def test_matches_a_no_parameter_command(self) -> None:
        self.llm.reply = _reply("look")
        result = self.runner.run("take a look around", self.session.id)
        self.assertIn(self.world.get_room(self.start_id).description, result)

    def test_matches_a_command_and_extracts_its_parameter(self) -> None:
        self.llm.reply = _reply("go", direction="north")
        result = self.runner.run("head north please", self.session.id)
        self.assertEqual(self.player.current_room_id, self.north_id)
        self.assertEqual(result, self.world.get_room(self.north_id).format())

    def test_missing_required_parameter_falls_through_to_the_commands_own_prompt(
        self,
    ) -> None:
        # The model matched "go" but couldn't find a direction in the
        # text -- LLMCommandRunner doesn't invent one; GoCommand's own
        # run_command handles the gap exactly like CommandRunner would.
        self.llm.reply = _reply("go")
        result = self.runner.run("which way do I go?", self.session.id)
        self.assertEqual(result, "Go where?")
        self.assertEqual(self.player.current_room_id, self.start_id)

    def test_matches_a_multi_word_verb_command_with_a_multi_word_argument(self) -> None:
        item = Item(id="sword", name="a rusty sword")
        self.world.get_room(self.start_id).items.append(item)
        self.llm.reply = _reply("pick up", item="rusty sword")

        result = self.runner.run("grab that rusty sword", self.session.id)

        self.assertEqual(result, "You pick up a rusty sword.")
        self.assertIn(item, self.player.inventory)

    def test_null_command_is_unknown(self) -> None:
        self.llm.reply = _reply(None)
        result = self.runner.run("dance", self.session.id)
        self.assertEqual(result, "Unknown command: 'dance'")

    def test_hallucinated_command_name_is_unknown(self) -> None:
        self.llm.reply = _reply("fly")
        result = self.runner.run("fly away", self.session.id)
        self.assertEqual(result, "Unknown command: 'fly away'")

    def test_malformed_json_reply_is_unknown(self) -> None:
        self.llm.reply = "not json at all"
        result = self.runner.run("look", self.session.id)
        self.assertEqual(result, "Unknown command: 'look'")

    def test_non_object_json_reply_is_unknown(self) -> None:
        self.llm.reply = "[1, 2, 3]"
        result = self.runner.run("look", self.session.id)
        self.assertEqual(result, "Unknown command: 'look'")

    def test_strips_markdown_code_fence_around_json(self) -> None:
        self.llm.reply = "```json\n" + _reply("look") + "\n```"
        result = self.runner.run("look", self.session.id)
        self.assertIn(self.world.get_room(self.start_id).description, result)

    def test_llm_request_error_falls_back_to_unknown_command(self) -> None:
        self.llm.error = LLMRequestError("provider down")
        result = self.runner.run("look", self.session.id)
        self.assertEqual(result, "Unknown command: 'look'")

    def test_ignores_parameters_not_declared_by_the_matched_command(self) -> None:
        # The model hallucinated an extra "speed" parameter go doesn't
        # declare -- it must be silently dropped, not crash or leak
        # into the reconstructed line.
        self.llm.reply = json.dumps(
            {"command": "go", "parameters": {"direction": "north", "speed": "fast"}}
        )
        result = self.runner.run("run north fast", self.session.id)
        self.assertEqual(self.player.current_room_id, self.north_id)
        self.assertEqual(result, self.world.get_room(self.north_id).format())


class LLMCommandRunnerLoggingTests(LLMCommandRunnerTestCase):
    def test_request_error_logs_a_warning(self) -> None:
        self.llm.error = LLMRequestError("provider down")
        with self.assertLogs("mud.commands.llm_runner", level="WARNING") as cm:
            self.runner.run("look", self.session.id)
        self.assertIn("LLM request failed", cm.output[0])
        self.assertIn("provider down", cm.output[0])

    def test_malformed_reply_logs_a_warning(self) -> None:
        self.llm.reply = "not json at all"
        with self.assertLogs("mud.commands.llm_runner", level="WARNING") as cm:
            self.runner.run("look", self.session.id)
        self.assertIn("Could not parse", cm.output[0])

    def test_explicit_null_command_logs_info_not_a_warning(self) -> None:
        # A well-formed "no command applies" isn't a parsing failure --
        # it shouldn't be logged like one.
        self.llm.reply = _reply(None)
        with self.assertLogs("mud.commands.llm_runner", level="INFO") as cm:
            self.runner.run("dance", self.session.id)
        self.assertTrue(all(not r.startswith("WARNING") for r in cm.output))
        self.assertIn("no matching command", "\n".join(cm.output))

    def test_hallucinated_command_logs_info(self) -> None:
        self.llm.reply = _reply("fly")
        with self.assertLogs("mud.commands.llm_runner", level="INFO") as cm:
            self.runner.run("fly away", self.session.id)
        self.assertIn("unregistered command", "\n".join(cm.output))

    def test_successful_match_logs_at_debug(self) -> None:
        self.llm.reply = _reply("go", direction="north")
        with self.assertLogs("mud.commands.llm_runner", level="DEBUG") as cm:
            self.runner.run("head north", self.session.id)
        self.assertIn("matched", "\n".join(cm.output))


class LLMCommandRunnerWithHelpTests(unittest.TestCase):
    """HelpCommand takes a `commands` list at construction, same as with
    CommandRunner -- confirms LLMCommandRunner composes with it fine."""

    def setUp(self) -> None:
        self.world, self.start_id, _ = build_two_room_world()
        self.sessions = SessionRegistry()
        self.repo = FakePlayerRepository()
        self.player = self.repo.create("alice", starting_room_id=self.start_id)
        self.world.place_player(self.player)
        self.session = self.sessions.create(self.player)

        commands = [LookCommand(self.sessions, self.world)]
        commands.append(HelpCommand(self.sessions, self.world, commands))
        self.llm = FakeLLMClient()
        self.runner = LLMCommandRunner(self.llm, commands)

    def test_matches_help(self) -> None:
        self.llm.reply = _reply("help")
        result = self.runner.run("what can I do?", self.session.id)
        self.assertIn("look", result)
        self.assertIn("help", result)


if __name__ == "__main__":
    unittest.main()
