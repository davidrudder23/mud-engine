"""The telnet server.

On connect: generate a session (random uuid) for the connection and
place it in a random room. From then on, every line the client sends is
handed to the CommandRunner, and whatever it returns is written back.
"""

from __future__ import annotations

import asyncio
import random

from mud.commands.command import Command
from mud.commands.go import GoCommand
from mud.commands.look import LookCommand
from mud.commands.runner import CommandRunner
from mud.session import SessionRegistry
from mud.world import World

DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 8023


def _build_commands(sessions: SessionRegistry) -> list[Command]:
    return [LookCommand(sessions), GoCommand()]


async def _handle_client(
    reader: asyncio.StreamReader,
    writer: asyncio.StreamWriter,
    world: World,
    sessions: SessionRegistry,
    command_runner: CommandRunner,
) -> None:
    peer = writer.get_extra_info("peername")
    print(f"Connection from {peer}", flush=True)

    room = random.choice(list(world.rooms.values()))
    session = sessions.create(room)

    try:
        writer.write((room.format() + "\r\n").encode())
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
    except (ConnectionError, asyncio.IncompleteReadError, ValueError):
        # ValueError: readline() hit its buffer limit without finding a
        # newline (asyncio.LimitOverrunError surfaces as ValueError here) —
        # treat it like any other client-caused disconnect.
        pass
    finally:
        sessions.remove(session.id)
        print(f"Connection closed: {peer}", flush=True)
        writer.close()
        try:
            await writer.wait_closed()
        except ConnectionError:
            pass


async def run(world: World, host: str = DEFAULT_HOST, port: int = DEFAULT_PORT) -> None:
    sessions = SessionRegistry()
    command_runner = CommandRunner(_build_commands(sessions))

    server = await asyncio.start_server(
        lambda r, w: _handle_client(r, w, world, sessions, command_runner),
        host,
        port,
    )
    addr = ", ".join(str(sock.getsockname()) for sock in server.sockets)
    print(f"MUD telnet server listening on {addr}", flush=True)
    async with server:
        await server.serve_forever()
