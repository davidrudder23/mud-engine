"""Exposes the game as MCP tools -- one per registered Command -- so an
MCP-capable client (Claude Desktop, Claude Code, etc.) can drive a
single Player as a game session, instead of a human connecting over
telnet.

One player per server process: there's no login flow here the way
telnet_server has one (an MCP client doesn't "connect" and get prompted
for a name) -- the player name comes from config at startup (see
mcp_server.py the entrypoint script, MUD_MCP_PLAYER_NAME), resolved via
telnet_server.resolve_player so a restart, a first-ever run, and a
saved-room-that-no-longer-exists are all handled identically to a
telnet login. `_build_commands` (telnet_server.py) builds the exact
same commands a telnet connection would get, bound to this one session.

Every tool takes exactly the parameters its Command declares
(Command.parameters()) and returns the same shape:

    {"message": "<run_command's own text>", "room": {...Room.to_dict()...}}

"message" is whatever the command itself would say (an error prompt, a
flavor line, `talk`'s NPC reply, ...); "room" is a fresh, structured
snapshot of wherever the player ends up standing, on every call -- the
JSON counterpart to what `look` shows a telnet player, so nothing
extra needs calling just to see the result of an action.

A tool function needs a *real*, inspectable signature -- not just
**kwargs -- for MCP to build a correct per-parameter JSON schema. Since
each command's parameter list is only known at runtime (from
Command.parameters()), `_make_tool_function` builds a generic function
and overrides its `__signature__` with `inspect.Parameter`s matching
that command; `inspect.signature()` (which MCP's schema builder uses)
returns an assigned `__signature__` as-is, without inspecting the
function's actual code -- see
https://docs.python.org/3/library/inspect.html#inspect.signature.
"""

from __future__ import annotations

import inspect
import logging
from typing import Any, Callable

from mcp.server.mcpserver import MCPServer

from mud.commands.command import Command
from mud.repositories.player_repository import PlayerRepository
from mud.session import Session, SessionRegistry
from mud.telnet_server import _build_commands, resolve_player
from mud.world import World

logger = logging.getLogger(__name__)


def _tool_name(command: Command) -> str:
    """MCP tool names are identifiers -- "pick up" isn't one, so spaces
    become underscores ("pick_up"). Purely a presentation-layer rename:
    Command.name() itself stays untouched since matches_verb/build_line
    (and the `help` listing) all still need the real, space-containing
    verb."""
    return command.name().replace(" ", "_")


def _tool_description(command: Command) -> str:
    # describe()'s player-facing text is column-padded for the `help`
    # listing (e.g. "go <dir>    moves..."); collapse that whitespace so
    # it doesn't look broken as a tool description.
    return " ".join(command.describe().split())


def _make_tool_function(
    command: Command, session: Session, world: World
) -> Callable[..., dict[str, Any]]:
    """Build the callable `add_tool` registers for `command`: runs it
    (via Command.build_line, same as LLMCommandRunner uses) against
    `session`'s player, and returns the {"message", "room"} envelope
    every tool shares (see module docstring)."""

    def run(**kwargs: str) -> dict[str, Any]:
        line = command.build_line(kwargs)
        message = command.run_command(line, session.id)
        room = world.get_room(session.player.current_room_id)
        return {"message": message, "room": room.to_dict()}

    # See the module docstring for why this (rather than real named
    # parameters in `run`'s own def) is what gives MCP a correct schema.
    run.__signature__ = inspect.Signature(
        parameters=[
            inspect.Parameter(
                p.name,
                inspect.Parameter.POSITIONAL_OR_KEYWORD,
                annotation=p.type,
                default=inspect.Parameter.empty if p.required else "",
            )
            for p in command.parameters()
        ],
        return_annotation=dict[str, Any],
    )
    run.__name__ = _tool_name(command)
    return run


def build_server(
    world: World, player_repo: PlayerRepository, player_name: str
) -> MCPServer:
    """Resolve `player_name` into a Player (creating it if needed),
    place it in the world, and build an MCPServer with one tool per
    Command `_build_commands` (telnet_server.py) would register for a
    telnet connection -- same commands, same behavior, different
    transport."""
    player = resolve_player(world, player_repo, player_name)
    world.place_player(player)

    sessions = SessionRegistry()
    session = sessions.create(player)
    commands = _build_commands(sessions, world, player_repo)

    server = MCPServer(
        "mud-engine",
        instructions=(
            "You are playing a text-based multiplayer dungeon (MUD) as "
            f"{player.name}. Each tool call runs one in-game command and "
            "returns {message, room}: `message` is that command's own "
            "reply, `room` is a fresh, structured snapshot of your "
            "current surroundings -- read it after every call instead "
            "of assuming what changed."
        ),
    )
    for command in commands:
        server.add_tool(
            _make_tool_function(command, session, world),
            name=_tool_name(command),
            description=_tool_description(command),
        )

    logger.info(
        "MCP server built: player=%s room=%s tools=%d",
        player.name, player.current_room_id, len(commands),
    )
    return server
