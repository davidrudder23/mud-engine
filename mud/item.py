"""Item: an object that lives in a room."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass
class Item:
    id: str
    name: str
    description: str = ""
