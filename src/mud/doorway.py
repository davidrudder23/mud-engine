"""Doorway: a connection from one room to another."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass
class Doorway:
    direction: str
    to: str  # room id of the destination
    description: str = ""

    def format(self) -> str:
        suffix = f" ({self.description})" if self.description else ""
        return f"{self.direction}{suffix}"

    def to_dict(self) -> dict:
        """JSON-serializable snapshot, for the MCP server (see
        mcp_server.py) -- the structured counterpart to format()."""
        return {"direction": self.direction, "to": self.to, "description": self.description}
