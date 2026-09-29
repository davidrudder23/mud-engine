"""Item: an object that lives in a room."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass
class Item:
    id: str
    name: str
    description: str = ""

    def format(self) -> str:
        return self.name

    def to_dict(self) -> dict:
        """JSON-serializable snapshot, for the MCP server (see
        mcp_server.py) -- the structured counterpart to format()."""
        return {"id": self.id, "name": self.name, "description": self.description}

    def get_commands(self) -> list[str]:
        """Verbs this item makes available, for `look`'s "you can try"
        hint. Every item can be picked up, so this is constant for now —
        it becomes real once items can restrict which of these apply
        (e.g. something bolted down)."""
        return ["pick up"]
