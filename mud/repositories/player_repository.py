"""Abstract interface for player persistence.

Game logic should depend on this interface, not on psycopg2 (or Postgres
at all) directly. That's what lets the storage backend change later — a
different database, a cache in front of it, sharding by player id,
whatever "scale" ends up meaning in practice — without touching anything
that uses a PlayerRepository.
"""

from __future__ import annotations

import uuid
from abc import ABC, abstractmethod

from mud.player import Player


class PlayerRepository(ABC):
    @abstractmethod
    def create(self, name: str, starting_room_id: str) -> Player:
        """Create and persist a new player. Raises DuplicatePlayerName if
        `name` is already taken."""

    @abstractmethod
    def get_by_id(self, player_id: uuid.UUID) -> Player | None: ...

    @abstractmethod
    def get_by_name(self, name: str) -> Player | None: ...

    @abstractmethod
    def save(self, player: Player) -> None:
        """Persist changes to an existing (already-created) player."""

    @abstractmethod
    def list_all(self) -> list[Player]: ...


class DuplicatePlayerName(Exception):
    """Raised by `create` when a player name is already taken."""
