"""End-to-end tests for the telnet server's connection handling: login,
the read/dispatch/respond loop, and cleanup on disconnect.

These drive `_login` and `_handle_client` directly with fake
asyncio-shaped reader/writer objects (test.fakes) instead of opening a
real socket -- that keeps the tests fast and deterministic while still
exercising the actual coroutine logic end-to-end (login -> place in
world -> run commands -> disconnect cleanup), the same code path a real
telnet client drives. Only the transport is faked; PostgresPlayerRepository
is swapped for FakePlayerRepository so no database is required either.
"""

from __future__ import annotations

import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from mud.commands.llm_runner import LLMCommandRunner
from mud.commands.runner import CommandRunner
from mud.llm import config
from mud.llm.config import LLMConfigError
from mud.session import SessionRegistry
from mud.telnet_server import _build_command_runner, _build_commands, _handle_client, _login

from test.fakes import FakePlayerRepository, FakeStreamReader, FakeStreamWriter
from test.helpers import build_two_room_world


def _lines(*strings: str) -> list[bytes]:
    return [f"{s}\r\n".encode() for s in strings]


class TelnetServerTestCase(unittest.IsolatedAsyncioTestCase):
    def setUp(self) -> None:
        self.world, self.start_id, self.north_id = build_two_room_world()
        self.repo = FakePlayerRepository()
        self.sessions = SessionRegistry()
        self.runner = CommandRunner(_build_commands(self.sessions, self.world, self.repo))

    async def run_client(self, lines: list[bytes], *, raise_after=None):
        """Simulate one connection sending `lines`, returns the
        (reader, writer) pair afterwards so the test can inspect the
        transcript and final state."""
        reader = FakeStreamReader(lines, raise_after=raise_after)
        writer = FakeStreamWriter()
        await _handle_client(
            reader, writer, self.world, self.repo, self.sessions, self.runner
        )
        return reader, writer


class FullSessionTests(TelnetServerTestCase):
    async def test_full_session_login_look_go_quit(self) -> None:
        _, writer = await self.run_client(
            _lines("alice", "look", "go north", "quit")
        )
        transcript = writer.text()

        self.assertIn("What is your name?", transcript)
        self.assertIn("Welcome, alice!", transcript)
        self.assertIn(self.world.get_room(self.start_id).description, transcript)
        self.assertIn(self.world.get_room(self.north_id).description, transcript)

        # quit must actually close the connection.
        self.assertTrue(writer.closed)
        self.assertTrue(writer.wait_closed_called)

        # And cleanup must have removed the session and the player from
        # whatever room they ended up in.
        self.assertEqual(len(self.sessions._sessions), 0)
        self.assertNotIn(
            "alice", [p.name for p in self.world.get_room(self.north_id).players]
        )

    async def test_move_is_persisted_across_the_session(self) -> None:
        await self.run_client(_lines("alice", "go north", "quit"))

        saved = self.repo.get_by_name("alice")
        self.assertEqual(saved.current_room_id, self.north_id)

    async def test_unknown_command_gets_a_reply_and_session_continues(self) -> None:
        _, writer = await self.run_client(_lines("alice", "dance", "look", "quit"))
        transcript = writer.text()
        self.assertIn("Unknown command: 'dance'", transcript)
        # The session kept going afterwards (look's reply is present too).
        self.assertIn(self.world.get_room(self.start_id).description, transcript)

    async def test_invalid_direction_does_not_move_player_or_close(self) -> None:
        _, writer = await self.run_client(_lines("alice", "go west", "quit"))
        self.assertIn("You can't go 'west' from here.", writer.text())
        saved = self.repo.get_by_name("alice")
        self.assertEqual(saved.current_room_id, self.start_id)

    async def test_blank_lines_are_ignored(self) -> None:
        _, writer = await self.run_client(_lines("alice", "", "   ", "look", "quit"))
        transcript = writer.text()
        # No "Unknown command" noise from the blank lines.
        self.assertNotIn("Unknown command", transcript)
        self.assertIn(self.world.get_room(self.start_id).description, transcript)


class DisconnectTests(TelnetServerTestCase):
    async def test_disconnect_before_giving_a_name(self) -> None:
        reader, writer = await self.run_client([])  # EOF immediately

        self.assertIn("What is your name?", writer.text())
        self.assertEqual(len(self.sessions._sessions), 0)
        # No player should have been created or placed anywhere.
        self.assertIsNone(self.repo.get_by_name(""))
        for room in self.world:
            self.assertEqual(room.players, [])

    async def test_disconnect_mid_session_cleans_up(self) -> None:
        # No "quit" -- the connection just drops (EOF) after "look".
        await self.run_client(_lines("alice", "look"))

        self.assertEqual(len(self.sessions._sessions), 0)
        for room in self.world:
            self.assertNotIn("alice", [p.name for p in room.players])

    async def test_blank_name_falls_back_to_adventurer(self) -> None:
        _, writer = await self.run_client(_lines("", "quit"))
        self.assertIn("Welcome, adventurer!", writer.text())

    async def test_unexpected_read_error_is_handled_like_a_disconnect(self) -> None:
        # e.g. a line longer than the stream's buffer limit surfaces as
        # ValueError from readline() -- must be treated like any other
        # client-caused disconnect, not crash the handler.
        reader, writer = await self.run_client(
            _lines("alice", "look"), raise_after=ValueError("line too long")
        )
        self.assertTrue(writer.closed)
        self.assertEqual(len(self.sessions._sessions), 0)


class ConcurrentSessionTests(TelnetServerTestCase):
    async def test_two_logins_same_name_are_independent_sessions(self) -> None:
        # Two "connections" logging in as "alice" concurrently (well,
        # sequentially here, but neither has quit/disconnected when the
        # second logs in) must be two independent Player instances, both
        # present in the room, each movable on its own.
        reader_a = FakeStreamReader(_lines("alice"))
        writer_a = FakeStreamWriter()
        player_a = await _login(reader_a, writer_a, self.world, self.repo)
        self.world.place_player(player_a)
        session_a = self.sessions.create(player_a)

        reader_b = FakeStreamReader(_lines("alice"))
        writer_b = FakeStreamWriter()
        player_b = await _login(reader_b, writer_b, self.world, self.repo)
        self.world.place_player(player_b)
        session_b = self.sessions.create(player_b)

        self.assertIsNot(player_a, player_b)
        self.assertEqual(player_a.id, player_b.id)  # same underlying row
        room = self.world.get_room(self.start_id)
        self.assertEqual(len(room.players), 2)

        # Moving one doesn't move the other.
        self.runner.run("go north", session_a.id)
        self.assertEqual(player_a.current_room_id, self.north_id)
        self.assertEqual(player_b.current_room_id, self.start_id)
        self.assertIn(player_b, self.world.get_room(self.start_id).players)
        self.assertIn(player_a, self.world.get_room(self.north_id).players)


class StaleRoomOnLoginTests(TelnetServerTestCase):
    async def test_login_resets_player_whose_saved_room_no_longer_exists(self) -> None:
        # Simulate a player who was last saved in a room the current
        # world no longer has (e.g. the map was redone).
        ghost = self.repo.create("bob", starting_room_id="a-room-that-got-deleted")

        _, writer = await self.run_client(_lines("bob", "quit"))

        # Sent back to the (deterministic) default start room instead of
        # crashing the login.
        default_room_id = next(iter(self.world.rooms.values())).id
        self.assertIn(f"[{default_room_id}]", writer.text())

        saved = self.repo.get_by_id(ghost.id)
        self.assertEqual(saved.current_room_id, default_room_id)


class LoggingTests(TelnetServerTestCase):
    """Login, command-issued, and logout log records -- the three
    things explicitly asked for, plus the stale-room-reset and
    connection-error records covered by their own dedicated tests."""

    async def test_login_and_command_and_logout_are_logged(self) -> None:
        with self.assertLogs("mud.telnet_server", level="INFO") as cm:
            await self.run_client(_lines("alice", "look", "quit"))

        joined = "\n".join(cm.output)
        self.assertIn("Login: player=alice", joined)
        self.assertIn("Command: session=", joined)
        self.assertIn("player=alice line='look'", joined)
        self.assertIn("Logout: session=", joined)
        self.assertIn("player=alice", joined)
        self.assertIn("reason=quit", joined)

    async def test_every_command_line_gets_its_own_log_record(self) -> None:
        with self.assertLogs("mud.telnet_server", level="INFO") as cm:
            await self.run_client(_lines("alice", "look", "go north", "quit"))

        command_records = [r for r in cm.output if "Command: " in r]
        self.assertEqual(len(command_records), 3)
        self.assertIn("line='look'", command_records[0])
        self.assertIn("line='go north'", command_records[1])
        self.assertIn("line='quit'", command_records[2])

    async def test_blank_lines_are_not_logged_as_commands(self) -> None:
        with self.assertLogs("mud.telnet_server", level="INFO") as cm:
            await self.run_client(_lines("alice", "", "   ", "look", "quit"))

        # Only "look" and "quit" are real command lines -- the blank
        # ones in between must not add extra records.
        command_records = [r for r in cm.output if "Command: " in r]
        self.assertEqual(len(command_records), 2)

    async def test_disconnect_without_quit_is_logged_with_that_reason(self) -> None:
        with self.assertLogs("mud.telnet_server", level="INFO") as cm:
            await self.run_client(_lines("alice", "look"))  # no quit -- just EOF

        joined = "\n".join(cm.output)
        self.assertIn("Logout:", joined)
        self.assertIn("reason=disconnect", joined)

    async def test_connection_error_is_logged_with_that_reason(self) -> None:
        with self.assertLogs("mud.telnet_server", level="WARNING") as cm:
            await self.run_client(
                _lines("alice", "look"), raise_after=ValueError("line too long")
            )

        joined = "\n".join(cm.output)
        self.assertIn("Connection error", joined)
        self.assertIn("line too long", joined)

    async def test_new_vs_existing_player_is_distinguished_in_the_login_log(self) -> None:
        self.repo.create("alice", starting_room_id=self.start_id)

        with self.assertLogs("mud.telnet_server", level="INFO") as cm:
            await self.run_client(_lines("alice", "quit"))

        joined = "\n".join(cm.output)
        self.assertIn("Login: player=alice new_player=False", joined)

    async def test_stale_room_reset_is_logged(self) -> None:
        self.repo.create("bob", starting_room_id="a-room-that-got-deleted")

        with self.assertLogs("mud.telnet_server", level="WARNING") as cm:
            await self.run_client(_lines("bob", "quit"))

        joined = "\n".join(cm.output)
        self.assertIn("saved room", joined)
        self.assertIn("no longer exists", joined)

    async def test_no_login_log_when_connection_drops_before_a_name_is_given(self) -> None:
        with self.assertLogs("mud.telnet_server", level="INFO") as cm:
            await self.run_client([])  # EOF immediately

        joined = "\n".join(cm.output)
        self.assertNotIn("Login:", joined)
        self.assertNotIn("Logout:", joined)
        self.assertIn("Connection opened", joined)
        self.assertIn("Connection closed", joined)


class BuildCommandRunnerTests(TelnetServerTestCase):
    """_build_command_runner's MUD_COMMAND_MATCHING switch. Points
    mud.llm.config.CONFIG_FILE at a scratch path that doesn't exist, so
    these never read the real llm.config.json in the project root."""

    def setUp(self) -> None:
        super().setUp()
        tmpdir = tempfile.TemporaryDirectory()
        self.addCleanup(tmpdir.cleanup)
        patcher = patch.object(config, "CONFIG_FILE", Path(tmpdir.name) / "llm.config.json")
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_defaults_to_command_runner(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            runner = _build_command_runner(self.sessions, self.world, self.repo)
        self.assertIsInstance(runner, CommandRunner)

    def test_llm_matching_env_var_selects_llm_command_runner(self) -> None:
        env = {"MUD_COMMAND_MATCHING": "llm", "MUD_LLM_API_KEY": "sk-test"}
        with patch.dict(os.environ, env, clear=True):
            runner = _build_command_runner(self.sessions, self.world, self.repo)
        self.assertIsInstance(runner, LLMCommandRunner)

    def test_llm_matching_is_case_and_whitespace_insensitive(self) -> None:
        env = {"MUD_COMMAND_MATCHING": "  LLM  ", "MUD_LLM_API_KEY": "sk-test"}
        with patch.dict(os.environ, env, clear=True):
            runner = _build_command_runner(self.sessions, self.world, self.repo)
        self.assertIsInstance(runner, LLMCommandRunner)

    def test_llm_matching_without_api_key_raises_at_startup(self) -> None:
        # Fails fast when the server starts, rather than on the first
        # line a player types.
        with patch.dict(os.environ, {"MUD_COMMAND_MATCHING": "llm"}, clear=True):
            with self.assertRaises(LLMConfigError):
                _build_command_runner(self.sessions, self.world, self.repo)


if __name__ == "__main__":
    unittest.main()
