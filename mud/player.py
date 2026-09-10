"""Player: a character in the world.

Unlike rooms/items (static, authored content loaded from JSON), players
are dynamic, multi-user state — created and changed constantly as people
play — so they're persisted in the database instead.
"""

from __future__ import annotations

import uuid
from dataclasses import dataclass
from datetime import datetime


@dataclass
class Player:
    name: str
    current_room_id: str
    id: uuid.UUID | None = None
    created_at: datetime | None = None
    updated_at: datetime | None = None
