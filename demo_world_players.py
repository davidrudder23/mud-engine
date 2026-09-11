#!/usr/bin/env python3
"""Show room <-> player occupancy staying in sync.

Loads the JSON world, creates a couple of demo players in Postgres,
places them into the (in-memory) world, moves one, and prints room
occupant lists before/after to show they always agree with each
player's current_room_id.

Run `python3 migrate.py` once first to create the schema.
"""

import uuid
from pathlib import Path

from mud.loader import load_world
from mud.repositories.postgres_player_repository import PostgresPlayerRepository

WORLD_DIR = Path(__file__).parent / "world"


def print_occupancy(world) -> None:
    for room in world:
        names = [p.name for p in room.players]
        print(f"  [{room.id}] occupants: {names or '(empty)'}")


def main() -> None:
    world = load_world(WORLD_DIR)
    repo = PostgresPlayerRepository()

    suffix = uuid.uuid4().hex[:8]
    alice = repo.create(f"alice-{suffix}", starting_room_id="room-1")
    bob = repo.create(f"bob-{suffix}", starting_room_id="room-1")

    # Bring persisted players into the running world.
    world.place_player(alice)
    world.place_player(bob)

    print("After both players join in room-1:")
    print_occupancy(world)

    world.move_player(bob, "room-2")
    repo.save(bob)  # persist the move so it survives a restart

    print("\nAfter bob moves to room-2:")
    print_occupancy(world)

    room_1 = world.get_room("room-1")
    room_2 = world.get_room("room-2")
    assert alice in room_1.players and alice not in room_2.players
    assert bob in room_2.players and bob not in room_1.players
    assert bob.current_room_id == "room-2"
    print("\nOK: room occupant lists and player.current_room_id agree.")


if __name__ == "__main__":
    main()
