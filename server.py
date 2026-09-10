#!/usr/bin/env python3
"""Run the MUD telnet server.

Connect with: telnet localhost 8023

For now this just shows a random room on connect — no commands yet
(that's the next step).

Config via env vars:
    MUD_TELNET_HOST   default 127.0.0.1 (loopback only; set to 0.0.0.0
                       to accept connections from other machines)
    MUD_TELNET_PORT   default 8023
"""

import asyncio
import os
from pathlib import Path

from mud.loader import load_world
from mud.telnet_server import DEFAULT_HOST, DEFAULT_PORT, run

WORLD_DIR = Path(__file__).parent / "world" / "rooms"


def main() -> None:
    world = load_world(WORLD_DIR)
    host = os.environ.get("MUD_TELNET_HOST", DEFAULT_HOST)
    port = int(os.environ.get("MUD_TELNET_PORT", DEFAULT_PORT))

    try:
        asyncio.run(run(world, host, port))
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
