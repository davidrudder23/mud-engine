"""Runs WanderingNPCs: every `npc.move_interval` seconds, one moves
through a random doorway of whatever room it's currently standing in.

`wander_once` is the actual logic (pure, synchronous, easy to test
without waiting on real time); `run_forever` is the long-lived per-NPC
loop around it that `telnet_server.run()` starts one of -- via
`asyncio.create_task` -- for every WanderingNPC the loaded world has,
for as long as the server runs.

Deliberately one task per wandering NPC rather than one shared loop
ticking all of them: each NPC can have its own move_interval, a stuck
or slow move for one NPC can't delay another's, and cancelling one
(e.g. if it's ever despawned) doesn't touch the rest.
"""

from __future__ import annotations

import asyncio
import logging
import random

from mud.npc import WanderingNPC
from mud.world import World

logger = logging.getLogger(__name__)


def wander_once(npc: WanderingNPC, world: World, *, rng=random) -> None:
    """Move `npc` through one random doorway of its current room. A
    room with no doorways is a no-op -- there's nowhere to wander to.

    `rng` defaults to the `random` module itself (its module-level
    `choice` makes it a drop-in "random source"); tests pass a
    `random.Random(seed)` instead for a deterministic pick.
    """
    room = world.get_room(npc.room_id)
    if not room.doorways:
        return

    doorway = rng.choice(room.doorways)
    world.move_npc(npc, doorway.to)
    logger.info("NPC wandered: npc=%s from=%s to=%s", npc.name, room.id, doorway.to)


async def run_forever(npc: WanderingNPC, world: World) -> None:
    """Move `npc` once every npc.move_interval seconds, forever -- until
    this task is cancelled (telnet_server.run() cancels every one of
    these it started when the server shuts down)."""
    while True:
        await asyncio.sleep(npc.move_interval)
        wander_once(npc, world)
