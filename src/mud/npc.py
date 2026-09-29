"""NPC: a non-player character. Starting with "helpers" (see
game-idea.md); enemies come later.

An NPC object plays two roles, the same class either way:

1. A **definition** -- static, authored content like Item/Room/
   Structure (id/name/description), loaded once per id from
   world/npcs/*.json (see loader.py) and held in World.npcs. This is
   the template: what a "tavern-keeper" or "shopkeeper" *is*.
2. A spawned **instance** -- placed into a specific room's `Room.npcs`
   via `spawn()` (below), with its own independent, live state (right
   now just `inventory`, since that's the only thing anything mutates).

The two are deliberately the same class, not a definition/instance
split into separate types: spawning an NPC into a room is the same
kind of event logging a Player in is -- it gains a room, the same way
login gives a Player its first `current_room_id` -- plus its own copy
of the mutable fields. A definition is really just an NPC nobody has
spawned yet (`room_id` unset).

Unlike a Player (one instance per login, but the *same* one instance
stays in exactly one room for its whole session), an NPC definition can
back many simultaneous instances -- imagine the same "shopkeeper"
definition placed in five different rooms, or twice in one room. Each
`spawn()` call produces one, fully independent of the definition and of
every other spawn: trading away one shopkeeper's stock doesn't touch
another's, or the definition's own `inventory` (which nothing should
mutate directly -- it exists only as the template each spawn copies).

`eq=False` mirrors Player's exact reasoning (see player.py): without
it, dataclass's default field-by-field equality would treat two fresh
spawns of the same definition (identical id/name/description/hostile/
commands, and, immediately after spawning, identical-looking inventory
contents too) as equal, and a `room.npcs.remove(npc)` or `if npc not in
room.npcs` check could silently act on the wrong one.
"""

from __future__ import annotations

from dataclasses import dataclass, field, fields

from mud.item import Item


@dataclass(eq=False)
class NPC:
    id: str
    name: str
    description: str = ""
    # Helper vs. enemy, eventually. Nothing reads this yet -- no combat,
    # no behavior branches on it -- it just exists on the object ahead
    # of anything needing to act on it, the same way match_room started
    # as an always-True stub before rooms could restrict commands (see
    # commands/command.py).
    hostile: bool = False
    # Verbs this NPC makes available beyond the baseline in
    # get_commands() (e.g. "trade") -- mirrors Room.commands: a plain
    # list of verb strings, authored/assigned directly rather than
    # derived from anything else. Nothing consumes an entry here unless
    # a matching Command exists (today, only "talk" does).
    commands: list[str] = field(default_factory=list)
    # What this NPC is carrying -- what a helper might hand over, or a
    # shopkeeper might sell. Live, in-memory state, same as
    # Player.inventory and Room.players; not persisted.
    inventory: list[Item] = field(default_factory=list)
    # Which room this instance stands in -- unset (None) on a
    # definition, which isn't standing anywhere; set by spawn() below.
    # Mirrors Player.current_room_id, except a Player's is required
    # (a Player always has a room) where an NPC's is optional (a
    # definition doesn't, yet).
    room_id: str | None = None

    def format(self) -> str:
        return self.name

    def spawn(self, room_id: str) -> NPC:
        """Create a new, independent NPC instance from this definition
        (or from any existing instance -- spawning a spawn works fine,
        it just copies whatever's currently on this object) and place
        it in `room_id`: same id/name/description/hostile/commands (and
        whatever extra fields a subclass adds -- see WanderingNPC.
        move_interval), but its own fresh inventory (deep-copied, not
        shared -- see the module docstring), its own identity distinct
        from every other spawn (see `eq=False` above), and `room_id` set
        to wherever it was just placed. This is how one npcs/*.json
        definition becomes N independent placements -- called once per
        room `npcs` JSON reference in loader.py, so the same id listed
        in five rooms (or twice in one) yields five (or two) fully
        separate NPCs, each knowing which room it's actually in.

        Works for `NPC` and every subclass without needing an override:
        it copies whatever fields `type(self)` actually declares (via
        `dataclasses.fields`), special-casing only the two that need a
        real copy rather than a shared reference (`commands`,
        `inventory`) and the one this call itself is setting
        (`room_id`). A subclass adding a plain field (a number, a
        string, ...) just gets it carried over automatically; one
        adding another *mutable* field would need to override this to
        copy it properly, the same reason `commands`/`inventory` are
        special-cased here instead of just being copied by reference.

        This only builds the instance -- the caller still adds it to
        that room's `Room.npcs` (loader.py does, right after calling
        this), the same way World.place_player is a separate step from
        constructing a Player.
        """
        kwargs = {}
        for f in fields(self):
            if f.name == "commands":
                kwargs[f.name] = list(self.commands)
            elif f.name == "inventory":
                kwargs[f.name] = [
                    Item(id=item.id, name=item.name, description=item.description)
                    for item in self.inventory
                ]
            elif f.name == "room_id":
                kwargs[f.name] = room_id
            else:
                kwargs[f.name] = getattr(self, f.name)
        return type(self)(**kwargs)

    def to_dict(self) -> dict:
        """JSON-serializable snapshot, for the MCP server (see
        mcp_server.py) -- the structured counterpart to format()."""
        return {
            "id": self.id,
            "name": self.name,
            "description": self.description,
            "hostile": self.hostile,
            "commands": list(self.commands),
            "inventory": [item.to_dict() for item in self.inventory],
        }

    def get_commands(self) -> list[str]:
        """Verbs this NPC offers, for `look`'s "you can try" hint once
        it's placed in a room (see Room.get_commands()). Always includes
        "talk" -- every NPC can be talked to, the same way every Item
        can always be picked up (see item.py) -- plus whatever's
        authored onto `commands`, deduplicated and order-preserved.
        Nothing is derived from `inventory` yet, since what it should
        offer (trade? steal?) isn't designed yet.
        """
        return list(dict.fromkeys(["talk", *self.commands]))


@dataclass(eq=False)
class WanderingNPC(NPC):
    """An NPC that, left alone, moves through a random doorway of
    whatever room it's currently in every `move_interval` seconds --
    Carrot's beat, or any future patrol/wildlife behavior. The moving
    itself doesn't live here (an NPC is still just data, like every
    other content class in this codebase) -- see mud/npc_patrol.py,
    which runs one background task per WanderingNPC the loaded world
    has (started in telnet_server.run()) that calls World.move_npc on
    this timer.

    A definition's `move_interval` is copied to every instance
    spawn() creates, same as every other field -- see NPC.spawn(),
    which needs no override for this since move_interval is a plain
    value, not a mutable container.
    """

    move_interval: float = 120.0

    def to_dict(self) -> dict:
        return {**super().to_dict(), "move_interval": self.move_interval}
