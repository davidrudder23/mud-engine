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

import unittest

from mud.session import SessionRegistry
from mud.telnet_server import _build_commands, _handle_client, _login
from mud.commands.runner import CommandRunner

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


if __name__ == "__main__":
    unittest.main()
