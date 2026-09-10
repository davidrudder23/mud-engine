#!/usr/bin/env python3
"""Exercise player creation/lookup/persistence against Postgres.

Run `python3 migrate.py` once first to create the schema.
"""

import uuid

from mud.repositories.player_repository import DuplicatePlayerName
from mud.repositories.postgres_player_repository import PostgresPlayerRepository


def main() -> None:
    repo = PostgresPlayerRepository()

    name = f"demo-{uuid.uuid4().hex[:8]}"
    player = repo.create(name, starting_room_id="room-1")
    print(f"Created player: {player}")

    fetched = repo.get_by_id(player.id)
    print(f"Fetched by id:  {fetched}")

    player.current_room_id = "room-2"
    repo.save(player)
    moved = repo.get_by_name(name)
    print(f"After move:     {moved}")

    try:
        repo.create(name, starting_room_id="room-1")
        print("ERROR: should have raised DuplicatePlayerName")
    except DuplicatePlayerName as e:
        print(f"OK, got expected error: {e}")

    print(f"\nAll players ({len(repo.list_all())}):")
    for p in repo.list_all():
        print(f"  - {p.name} in {p.current_room_id}")


if __name__ == "__main__":
    main()
