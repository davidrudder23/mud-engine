"""Tests for the MCP server: building one tool per command from
_build_commands (telnet_server.py's exact command list) and each tool's
schema, dispatch, and {"message", "room"} return shape.

Drives the real mcp.server.mcpserver.MCPServer (list_tools/call_tool),
not a fake -- these exercise the actual schema MCP builds from each
tool's overridden __signature__, not just that our own wrapper function
returns the right dict. No stdio transport, no subprocess: call_tool()
is a plain coroutine.
"""

from __future__ import annotations

import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from mcp.server.mcpserver.exceptions import ToolError

from mud.item import Item
from mud.llm import config
from mud.mcp_server import build_server
from mud.npc import NPC

from test.fakes import FakePlayerRepository
from test.helpers import build_two_room_world


class MCPServerTestCase(unittest.IsolatedAsyncioTestCase):
    def setUp(self) -> None:
        self.world, self.start_id, self.north_id = build_two_room_world()
        self.repo = FakePlayerRepository()
        self.server = build_server(self.world, self.repo, "alice")
        self.room = self.world.get_room(self.start_id)


class ToolRegistrationTests(MCPServerTestCase):
    async def test_registers_one_tool_per_command_including_multi_word_verbs(self) -> None:
        tools = {t.name for t in await self.server.list_tools()}
        self.assertEqual(
            tools,
            {"look", "go", "quit", "pick_up", "drop", "order", "talk", "help"},
        )

    async def test_tool_description_is_describe_with_whitespace_collapsed(self) -> None:
        tools = {t.name: t for t in await self.server.list_tools()}
        self.assertEqual(tools["look"].description, "look re-describes the room you're in")

    async def test_no_parameter_command_has_an_empty_schema(self) -> None:
        tools = {t.name: t for t in await self.server.list_tools()}
        schema = tools["look"].input_schema
        self.assertEqual(schema["properties"], {})
        self.assertNotIn("required", schema)

    async def test_required_parameter_is_a_required_string_property(self) -> None:
        tools = {t.name: t for t in await self.server.list_tools()}
        schema = tools["go"].input_schema
        self.assertEqual(schema["properties"]["direction"]["type"], "string")
        self.assertEqual(schema["required"], ["direction"])

    async def test_multi_word_verb_tool_name_has_no_space(self) -> None:
        tools = {t.name for t in await self.server.list_tools()}
        self.assertIn("pick_up", tools)
        self.assertNotIn("pick up", tools)


class ToolCallShapeTests(MCPServerTestCase):
    """Every tool call must return the same {"message", "room"} envelope,
    regardless of which command it dispatches to."""

    async def test_result_has_message_and_room_keys_only(self) -> None:
        result = await self.server.call_tool("look", {})
        self.assertFalse(result.is_error)
        self.assertEqual(set(result.structured_content), {"message", "room"})

    async def test_message_is_the_commands_own_text(self) -> None:
        result = await self.server.call_tool("quit", {})
        self.assertEqual(result.structured_content["message"], "")

    async def test_room_matches_room_to_dict(self) -> None:
        result = await self.server.call_tool("look", {})
        self.assertEqual(result.structured_content["room"], self.room.to_dict())

    async def test_structured_content_is_also_present_as_text(self) -> None:
        # Belt and suspenders for clients that only read text content
        # blocks: the same JSON should be readable there too.
        result = await self.server.call_tool("look", {})
        self.assertEqual(len(result.content), 1)
        self.assertIn('"message"', result.content[0].text)
        self.assertIn('"room"', result.content[0].text)


class ToolDispatchTests(MCPServerTestCase):
    async def test_look_returns_the_current_room(self) -> None:
        result = await self.server.call_tool("look", {})
        self.assertEqual(result.structured_content["room"]["id"], self.start_id)

    async def test_go_moves_the_player_and_returns_the_new_room(self) -> None:
        result = await self.server.call_tool("go", {"direction": "north"})
        self.assertEqual(result.structured_content["room"]["id"], self.north_id)
        self.assertEqual(
            self.repo.get_by_name("alice").current_room_id, self.north_id
        )

    async def test_go_without_a_valid_direction_does_not_move(self) -> None:
        result = await self.server.call_tool("go", {"direction": "west"})
        self.assertEqual(result.structured_content["message"], "You can't go 'west' from here.")
        self.assertEqual(result.structured_content["room"]["id"], self.start_id)

    async def test_pick_up_and_drop_round_trip_via_room_contents(self) -> None:
        self.room.items.append(Item(id="rock", name="a rock"))

        picked_up = await self.server.call_tool("pick_up", {"item": "rock"})
        self.assertEqual(picked_up.structured_content["room"]["items"], [])

        dropped = await self.server.call_tool("drop", {"item": "rock"})
        self.assertEqual(len(dropped.structured_content["room"]["items"]), 1)

    async def test_talk_without_llm_config_falls_back_gracefully(self) -> None:
        # talk's TalkCommand is built (via _build_commands) with no
        # LLMClient injected, so it resolves one for real via
        # mud.llm.config -- isolate that from the actual project
        # llm.config.json / ambient env vars, same as test_llm.py, so
        # this can't make a real network call regardless of local setup.
        self.room.npcs.append(NPC(id="old-tom", name="Old Tom"))
        with tempfile.TemporaryDirectory() as tmpdir:
            with (
                patch.object(config, "CONFIG_FILE", Path(tmpdir) / "llm.config.json"),
                patch.dict(os.environ, {}, clear=True),
            ):
                result = await self.server.call_tool("talk", {"npc": "tom"})
        self.assertFalse(result.is_error)
        self.assertEqual(
            result.structured_content["message"], "Old Tom doesn't seem to hear you."
        )

    async def test_help_lists_every_tools_command(self) -> None:
        result = await self.server.call_tool("help", {})
        message = result.structured_content["message"]
        for verb in ("look", "go <dir>", "quit", "pick up <item>", "drop <item>"):
            self.assertIn(verb, message)

    async def test_unknown_tool_name_is_rejected_by_mcp_itself(self) -> None:
        with self.assertRaises(ToolError):
            await self.server.call_tool("dance", {})


if __name__ == "__main__":
    unittest.main()
