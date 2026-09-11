#!/usr/bin/env python3
"""Load the example world and print it out.

No commands, no verbs — this just proves the JSON -> World pipeline works.
"""

from pathlib import Path

from mud.loader import load_world

WORLD_DIR = Path(__file__).parent / "world"


def main() -> None:
    world = load_world(WORLD_DIR)
    print(f"Loaded {len(world)} room(s) from {WORLD_DIR}\n")

    for room in world:
        print(f"[{room.id}] {room.description}")

        if room.items:
            print("  items:")
            for item in room.items:
                print(f"    - {item.name} ({item.id}): {item.description}")

        if room.doorways:
            print("  doorways:")
            for doorway in room.doorways:
                print(f"    - {doorway.direction} -> {doorway.to}: {doorway.description}")

        print()


if __name__ == "__main__":
    main()
