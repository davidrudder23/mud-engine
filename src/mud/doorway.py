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
