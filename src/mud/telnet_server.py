"""The telnet server.

On connect: ask for a name, get-or-create the matching Player (via
PlayerRepository), place it in the world, and start a Session tied to
that Player. From then on, every line the client sends is handed to the
CommandRunner, and whatever it returns is written back.

Logging in twice under the same name (two connections) is allowed on
purpose: each gets its own Session and its own Player *instance* for
the same underlying row (see mud/player.py) — so the same-named player
can appear twice in a room at once, moving independently. Not a bug.

There's no real auth: whatever name you type, you get (or become) that
player. No password, no session-per-name limit.
"""

from __future__ import annotations

import asyncio

from mud.commands.command import Command
from mud.commands.drop import DropCommand
from mud.commands.go import GoCommand
from mud.commands.help import HelpCommand
from mud.commands.look import LookCommand
from mud.commands.order import OrderCommand
from mud.commands.pick_up import PickUpCommand
from mud.commands.quit import QuitCommand
from mud.commands.runner import CommandRunner
from mud.player import Player
from mud.repositories.player_repository import DuplicatePlayerName, PlayerRepository
from mud.repositories.postgres_player_repository import PostgresPlayerRepository
from mud.session import SessionRegistry
from mud.world import World

DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 8023


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
    ]
    # HelpCommand lists `commands` at run time, after this function has
    # returned and the list below is complete -- appending itself last
    # (into the same list it was handed) is what makes `help` list
    # itself too.
    commands.append(HelpCommand(sessions, world, commands))
    return commands


def _default_starting_room_id(world: World) -> str:
    """Arbitrary but deterministic: the first loaded room. There's no
    "spawn point" concept yet — if the world ever needs one, mark it in
    JSON rather than relying on load order.
    """
    return next(iter(world.rooms.values())).id


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

    player = player_repo.get_by_name(name)
    if player is None:
        try:
            player = player_repo.create(
                name, starting_room_id=_default_starting_room_id(world)
            )
        except DuplicatePlayerName:
            # Someone else's login just created it between our check and
            # our insert -- fetch that row instead of failing.
            player = player_repo.get_by_name(name)

    if player.current_room_id not in world.rooms:
        # The world changed since this player last saved (e.g. the map
        # was redone) and their room no longer exists. Send them back to
        # the default start rather than crashing the login.
        player.current_room_id = _default_starting_room_id(world)
        player_repo.save(player)

    return player


async def _handle_client(
    reader: asyncio.StreamReader,
    writer: asyncio.StreamWriter,
    world: World,
    player_repo: PlayerRepository,
    sessions: SessionRegistry,
    command_runner: CommandRunner,
) -> None:
    peer = writer.get_extra_info("peername")
    print(f"Connection from {peer}", flush=True)

    session = None
    try:
        player = await _login(reader, writer, world, player_repo)
        if player is None:
            return  # disconnected before giving a name

        world.place_player(player)
        session = sessions.create(player)

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

            response = command_runner.run(line, session.id)
            writer.write((response + "\r\n").encode())
            await writer.drain()

            if session.should_close:
                break
    except (ConnectionError, asyncio.IncompleteReadError, ValueError):
        # ValueError: readline() hit its buffer limit without finding a
        # newline (asyncio.LimitOverrunError surfaces as ValueError here) —
        # treat it like any other client-caused disconnect.
        pass
    finally:
        if session is not None:
            world.remove_player(session.player)
            sessions.remove(session.id)
        print(f"Connection closed: {peer}", flush=True)
        writer.close()
        try:
            await writer.wait_closed()
        except ConnectionError:
            pass


async def run(world: World, host: str = DEFAULT_HOST, port: int = DEFAULT_PORT) -> None:
    sessions = SessionRegistry()
    player_repo = PostgresPlayerRepository()
    command_runner = CommandRunner(_build_commands(sessions, world, player_repo))

    server = await asyncio.start_server(
        lambda r, w: _handle_client(r, w, world, player_repo, sessions, command_runner),
        host,
        port,
    )
    addr = ", ".join(str(sock.getsockname()) for sock in server.sockets)
    print(f"MUD telnet server listening on {addr}", flush=True)
    async with server:
        await server.serve_forever()
