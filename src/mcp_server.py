#!/usr/bin/env python3
"""Run the MUD as an MCP server (stdio transport) -- one tool per
command, one player for the whole process. See mud/mcp_server.py for
how tools are built from commands.

An MCP client (Claude Desktop, Claude Code, ...) spawns this process
and talks JSON-RPC over its stdin/stdout, so nothing here should ever
write to stdout directly -- logging goes to stderr (see
mud.logging_config), same as the telnet server.

Config via env vars:
    MUD_MCP_PLAYER_NAME   default "adventurer" -- the single Player
                          this server plays as for its whole lifetime.
                          Resolved the same way a telnet login resolves
                          a name: existing player if one exists, a new
                          one (in the default starting room) otherwise.
"""

import logging
import os
from pathlib import Path

from mud.loader import load_world
from mud.logging_config import configure_logging
from mud.mcp_server import build_server
from mud.repositories.postgres_player_repository import PostgresPlayerRepository

WORLD_DIR = Path(__file__).parent / "world"

logger = logging.getLogger(__name__)


def main() -> None:
    configure_logging()

    world = load_world(WORLD_DIR)
    logger.info(
        "Loaded world: %d structures, %d rooms, %d npcs",
        len(world.structures), len(world.rooms), len(world.npcs),
    )

    player_repo = PostgresPlayerRepository()
    player_name = os.environ.get("MUD_MCP_PLAYER_NAME", "adventurer")

    server = build_server(world, player_repo, player_name)
    server.run(transport="stdio")


if __name__ == "__main__":
    main()
