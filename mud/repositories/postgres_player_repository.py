"""Postgres implementation of PlayerRepository."""

from __future__ import annotations

import uuid

import psycopg2.errors

from mud.db import get_connection
from mud.player import Player
from mud.repositories.player_repository import DuplicatePlayerName, PlayerRepository

_SELECT_COLUMNS = "id, name, current_room_id, created_at, updated_at"


def _row_to_player(row) -> Player:
    player_id, name, current_room_id, created_at, updated_at = row
    return Player(
        # psycopg2 doesn't auto-cast the uuid column to uuid.UUID (that
        # needs psycopg2.extras.register_uuid()); cast explicitly here so
        # Player.id always matches its declared type.
        id=uuid.UUID(player_id) if isinstance(player_id, str) else player_id,
        name=name,
        current_room_id=current_room_id,
        created_at=created_at,
        updated_at=updated_at,
    )


class PostgresPlayerRepository(PlayerRepository):
    def create(self, name: str, starting_room_id: str) -> Player:
        with get_connection() as conn:
            with conn.cursor() as cur:
                try:
                    cur.execute(
                        f"""
                        INSERT INTO players (name, current_room_id)
                        VALUES (%s, %s)
                        RETURNING {_SELECT_COLUMNS}
                        """,
                        (name, starting_room_id),
                    )
                except psycopg2.errors.UniqueViolation:
                    raise DuplicatePlayerName(
                        f"Player name already taken: {name!r}"
                    ) from None
                return _row_to_player(cur.fetchone())

    def get_by_id(self, player_id: uuid.UUID) -> Player | None:
        with get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    f"SELECT {_SELECT_COLUMNS} FROM players WHERE id = %s",
                    (str(player_id),),
                )
                row = cur.fetchone()
                return _row_to_player(row) if row else None

    def get_by_name(self, name: str) -> Player | None:
        with get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    f"SELECT {_SELECT_COLUMNS} FROM players WHERE name = %s",
                    (name,),
                )
                row = cur.fetchone()
                return _row_to_player(row) if row else None

    def save(self, player: Player) -> None:
        if player.id is None:
            raise ValueError("Cannot save a player that hasn't been created yet")
        with get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """
                    UPDATE players
                    SET current_room_id = %s, updated_at = now()
                    WHERE id = %s
                    """,
                    (player.current_room_id, str(player.id)),
                )

    def list_all(self) -> list[Player]:
        with get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    f"SELECT {_SELECT_COLUMNS} FROM players ORDER BY created_at"
                )
                return [_row_to_player(row) for row in cur.fetchall()]
