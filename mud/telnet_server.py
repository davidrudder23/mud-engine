"""A minimal telnet server.

Right now: on connect, show a random room and keep the connection open.
No command parsing yet — that's the next step, and it'll replace the
"just wait for disconnect" line below with an actual read loop.
"""

from __future__ import annotations

import asyncio
import random

from mud.room import Room
from mud.world import World

DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 8023


def format_room(room: Room) -> str:
    """Render a room as telnet-friendly text (CRLF line endings)."""
    lines = [f"[{room.id}]", room.description]

    if room.items:
        lines.append("You see:")
        lines.extend(f"  - {item.name}" for item in room.items)

    if room.doorways:
        lines.append("Doorways:")
        for doorway in room.doorways:
            suffix = f" ({doorway.description})" if doorway.description else ""
            lines.append(f"  - {doorway.direction}{suffix}")

    return "\r\n".join(lines) + "\r\n"


async def _handle_client(
    reader: asyncio.StreamReader, writer: asyncio.StreamWriter, world: World
) -> None:
    peer = writer.get_extra_info("peername")
    print(f"Connection from {peer}", flush=True)
    try:
        room = random.choice(list(world.rooms.values()))
        writer.write(format_room(room).encode())
        await writer.drain()

        # Nothing reads/parses commands yet. Block here until the client
        # disconnects instead of closing the socket out from under them.
        await reader.read()
    except (ConnectionError, asyncio.IncompleteReadError):
        pass
    finally:
        print(f"Connection closed: {peer}", flush=True)
        writer.close()
        try:
            await writer.wait_closed()
        except ConnectionError:
            pass


async def run(world: World, host: str = DEFAULT_HOST, port: int = DEFAULT_PORT) -> None:
    server = await asyncio.start_server(
        lambda r, w: _handle_client(r, w, world), host, port
    )
    addr = ", ".join(str(sock.getsockname()) for sock in server.sockets)
    print(f"MUD telnet server listening on {addr}", flush=True)
    async with server:
        await server.serve_forever()
