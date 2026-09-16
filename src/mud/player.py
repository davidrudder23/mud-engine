"""Player: a character in the world.

Unlike rooms/items (static, authored content loaded from JSON), players
are dynamic, multi-user state — created and changed constantly as people
play — so they're persisted in the database instead.

A Player is a distinct *instance*, not just a name: logging in twice
under the same name (from two different Sessions) deliberately gets two
separate Player objects that both refer to the same underlying row —
each moves independently and both can be in a room's occupant list at
once. `eq=False` makes that possible: without it, dataclass's default
field-by-field equality would treat two freshly-loaded instances of the
same row as equal, and World.place_player's `if player not in
room.players` check would silently treat the second login as already
present instead of adding it.
"""

from __future__ import annotations

import uuid
from dataclasses import dataclass, field
from datetime import datetime

from mud.item import Item


@dataclass(eq=False)
class Player:
    name: str
    current_room_id: str
    id: uuid.UUID | None = None
    created_at: datetime | None = None
    updated_at: datetime | None = None
    # What this player is carrying. Like Room.players, this is live,
    # in-memory state, not persisted to the players table -- picking
    # something up only lasts for the session, same as items aren't
    # otherwise mutable/persisted at all yet (see PickUpCommand).
    inventory: list[Item] = field(default_factory=list)

    def format(self) -> str:
        return self.name

    def get_commands(self) -> list[str]:
        """Verbs this player's presence makes available, for `look`'s
        "you can try" hint. Nothing yet (no talk/attack/etc.), so this
        is always empty for now."""
        return []
