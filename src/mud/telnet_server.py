"""The telnet server.

On connect: ask for a name, get-or-create the matching Player (via
PlayerRepository), place it in the world, and start a Session tied to
that Player. From then on, every line the client sends is handed to the
command runner, and whatever it returns is written back.

Logging in twice under the same name (two connections) is allowed on
purpose: each gets its own Session and its own Player *instance* for
the same underlying row (see mud/player.py) — so the same-named player
can appear twice in a room at once, moving independently. Not a bug.

There's no real auth: whatever name you type, you get (or become) that
player. No password, no session-per-name limit.

Two command-matching strategies are available -- CommandRunner (plain
matches_verb prefix check) and LLMCommandRunner (an LLM decides which
command a line invokes and extracts its parameters; see
commands/llm_runner.py). Which one `run()` builds is controlled by
MUD_COMMAND_MATCHING: "verb" (default) or "llm". LLM matching needs a
configured LLMClient (see mud/llm/config.py) -- get_llm_client() raises
LLMConfigError immediately at startup if one isn't set up, rather than
failing on the first line someone types.
"""

from __future__ import annotations

import asyncio
import logging
import os

from mud.commands.command import Command
from mud.commands.drop import DropCommand
from mud.commands.go import GoCommand
from mud.commands.help import HelpCommand
from mud.commands.llm_runner import LLMCommandRunner
from mud.commands.look import LookCommand
from mud.commands.order import OrderCommand
from mud.commands.pick_up import PickUpCommand
from mud.commands.quit import QuitCommand
from mud.commands.runner import CommandRunner
from mud.commands.talk import TalkCommand
from mud.llm.config import get_llm_client
from mud.npc import WanderingNPC
from mud.npc_patrol import run_forever as run_npc_wandering
from mud.player import Player
from mud.repositories.player_repository import DuplicatePlayerName, PlayerRepository
from mud.repositories.postgres_player_repository import PostgresPlayerRepository
from mud.session import SessionRegistry
from mud.world import World

DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 8023

logger = logging.getLogger(__name__)


def _build_commands(
    sessions: SessionRegistry, world: World, player_repo: PlayerRepository
) -> list[Command]:
    commands: list[Command] = [
        LookCommand(sessions, world),
        GoCommand(sessions, world, player_repo),
        QuitCommand(sessions),
        PickUpCommand(sessions, world),
        DropCommand(sessions, world),
        OrderCommand(),
        TalkCommand(sessions, world),
    ]
    # HelpCommand lists `commands` at run time, after this function has
    # returned and the list below is complete -- appending itself last
    # (into the same list it was handed) is what makes `help` list
    # itself too.
    commands.append(HelpCommand(sessions, world, commands))
    return commands


def _build_command_runner(
    sessions: SessionRegistry, world: World, player_repo: PlayerRepository
) -> CommandRunner | LLMCommandRunner:
    commands = _build_commands(sessions, world, player_repo)
    matching = os.environ.get("MUD_COMMAND_MATCHING", "verb").strip().lower()
    if matching == "llm":
        logger.info("Command matching: llm")
        return LLMCommandRunner(get_llm_client(), commands)
    logger.info("Command matching: verb")
    return CommandRunner(commands)


def _start_wandering_npcs(world: World) -> list[asyncio.Task]:
    """Start one background task (mud.npc_patrol.run_forever) per
    WanderingNPC currently placed in `world`, and return them so the
    caller can cancel them on shutdown. Called once, from run() -- a
    world with none just returns an empty list."""
    tasks = [
        asyncio.create_task(run_npc_wandering(npc, world))
        for room in world
        for npc in room.npcs
        if isinstance(npc, WanderingNPC)
    ]
    if tasks:
        logger.info("Started %d wandering NPC task(s)", len(tasks))
    return tasks


def _default_starting_room_id(world: World) -> str:
    """Arbitrary but deterministic: the first loaded room. There's no
    "spawn point" concept yet — if the world ever needs one, mark it in
    JSON rather than relying on load order.
    """
    return next(iter(world.rooms.values())).id


def resolve_player(world: World, player_repo: PlayerRepository, name: str) -> Player:
    """Get the Player named `name`, creating one (starting in the
    default room) if none exists yet. Also resets `current_room_id`
    (and persists that) if it points at a room the current world no
    longer has -- e.g. the map was redone since this player last saved.

    Split out of `_login` so the MCP server (see mcp_server.py, which
    gets its player's name from config instead of prompting over a
    connection) gets identical get-or-create and stale-room handling,
    not a second copy of it.
    """
    player = player_repo.get_by_name(name)
    is_new = player is None
    if player is None:
        try:
            player = player_repo.create(
                name, starting_room_id=_default_starting_room_id(world)
            )
        except DuplicatePlayerName:
            # Someone else's login just created it between our check and
            # our insert -- fetch that row instead of failing.
            logger.info("Duplicate-name race on create for %r; using the existing row", name)
            player = player_repo.get_by_name(name)

    if player.current_room_id not in world.rooms:
        # The world changed since this player last saved (e.g. the map
        # was redone) and their room no longer exists. Send them back to
        # the default start rather than crashing the login.
        stale_room_id = player.current_room_id
        player.current_room_id = _default_starting_room_id(world)
        player_repo.save(player)
        logger.warning(
            "Player %r's saved room %r no longer exists; reset to %r",
            name, stale_room_id, player.current_room_id,
        )

    logger.info("Login: player=%s new_player=%s room=%s", name, is_new, player.current_room_id)
    return player


async def _login(
    reader: asyncio.StreamReader,
    writer: asyncio.StreamWriter,
    world: World,
    player_repo: PlayerRepository,
) -> Player | None:
    """Prompt for a name and return the matching (or newly created)
    Player, or None if the connection dropped before answering."""
    writer.write(b"What is your name? ")
    await writer.drain()

    raw_name = await reader.readline()
    if not raw_name:
        return None

    name = raw_name.decode(errors="replace").strip()
    if not name:
        name = "adventurer"

    return resolve_player(world, player_repo, name)


async def _handle_client(
    reader: asyncio.StreamReader,
    writer: asyncio.StreamWriter,
    world: World,
    player_repo: PlayerRepository,
    sessions: SessionRegistry,
    command_runner: CommandRunner | LLMCommandRunner,
) -> None:
    peer = writer.get_extra_info("peername")
    logger.info("Connection opened: peer=%s", peer)

    session = None
    disconnect_reason = "disconnect"  # client EOF, the common case
    try:
        player = await _login(reader, writer, world, player_repo)
        if player is None:
            return  # disconnected before giving a name

        world.place_player(player)
        session = sessions.create(player)
        logger.info(
            "Session started: session=%s player=%s room=%s",
            session.id, player.name, player.current_room_id,
        )

        room = world.get_room(player.current_room_id)
        writer.write(
            (f"Welcome, {player.name}!\r\n\r\n" + room.format() + "\r\n").encode()
        )
        await writer.drain()

        while True:
            raw_line = await reader.readline()
            if not raw_line:  # EOF: client disconnected
                break

            line = raw_line.decode(errors="replace").strip()
            if not line:
                continue

            logger.info("Command: session=%s player=%s line=%r", session.id, player.name, line)
            response = command_runner.run(line, session.id)
            writer.write((response + "\r\n").encode())
            await writer.drain()

            if session.should_close:
                disconnect_reason = "quit"
                break
    except (ConnectionError, asyncio.IncompleteReadError, ValueError) as exc:
        # ValueError: readline() hit its buffer limit without finding a
        # newline (asyncio.LimitOverrunError surfaces as ValueError here) —
        # treat it like any other client-caused disconnect.
        disconnect_reason = "error"
        logger.warning("Connection error: peer=%s error=%s", peer, exc)
    finally:
        if session is not None:
            world.remove_player(session.player)
            sessions.remove(session.id)
            logger.info(
                "Logout: session=%s player=%s reason=%s room=%s",
                session.id, session.player.name, disconnect_reason,
                session.player.current_room_id,
            )
        logger.info("Connection closed: peer=%s", peer)
        writer.close()
        try:
            await writer.wait_closed()
        except ConnectionError:
            pass


async def run(world: World, host: str = DEFAULT_HOST, port: int = DEFAULT_PORT) -> None:
    sessions = SessionRegistry()
    player_repo = PostgresPlayerRepository()
    command_runner = _build_command_runner(sessions, world, player_repo)
    wander_tasks = _start_wandering_npcs(world)

    server = await asyncio.start_server(
        lambda r, w: _handle_client(r, w, world, player_repo, sessions, command_runner),
        host,
        port,
    )
    addr = ", ".join(str(sock.getsockname()) for sock in server.sockets)
    logger.info("MUD telnet server listening on %s", addr)
    try:
        async with server:
            await server.serve_forever()
    finally:
        for task in wander_tasks:
            task.cancel()
        if wander_tasks:
            await asyncio.gather(*wander_tasks, return_exceptions=True)
