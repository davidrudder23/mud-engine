"""The `talk` command: get one in-character line from an NPC in the
room.

Matches NPCs the same way `pick up`/`drop` match items: a
case-insensitive substring against the NPC's id or name. Accepts either
"talk <npc>" or "talk to <npc>" -- a leading "to " is stripped before
matching, purely so both read naturally; the verb match itself is just
"talk" (see matches_verb).

Single-shot: there's no conversation state and no player message to
respond to -- the model improvises one greeting/remark in character
from the NPC's name and description alone. A real back-and-forth (the
player saying something specific, the NPC replying to it, remembering
earlier turns) is a bigger feature this deliberately doesn't attempt
yet -- see npc.py.

The LLMClient is built lazily, on first use (via `get_llm_client`,
defaulting to mud.llm.config.get_llm_client -- a real network call
happens only inside `complete()`, not here), not at construction time --
so a server that never has anyone type `talk` never needs LLM config at
startup, the same way plain verb matching doesn't need it either (only
MUD_COMMAND_MATCHING="llm" does; see telnet_server.py). A failed build
(e.g. LLMConfigError, no API key configured) is retried on the next
`talk` call rather than cached, in case the config gets fixed mid-
session; a successful client is built once and reused after that.
"""

from __future__ import annotations

import logging
import uuid
from typing import Callable

from mud.commands.command import Command, Parameter, command_argument, matches_verb
from mud.llm.config import LLMConfigError, get_llm_client as _default_get_llm_client
from mud.llm.llm_client import LLMClient, LLMRequestError
from mud.room import Room
from mud.session import SessionRegistry
from mud.world import World

logger = logging.getLogger(__name__)

_GREETING_PROMPT = (
    "Greet the player who just approached you, briefly and in character. "
    "One or two sentences. No stage directions, no quotation marks around "
    "the whole reply."
)


class TalkCommand(Command):
    def __init__(
        self,
        sessions: SessionRegistry,
        world: World,
        get_llm_client: Callable[[], LLMClient] = _default_get_llm_client,
    ) -> None:
        self._sessions = sessions
        self._world = world
        self._get_llm_client = get_llm_client
        self._llm_client: LLMClient | None = None

    def name(self) -> str:
        return "talk"

    def match_command(self, line: str) -> bool:
        return matches_verb(line, self.name())

    def match_room(self, room: Room) -> bool:
        return True

    def describe(self) -> str:
        return "talk to <npc>   strikes up a conversation with an NPC in the room"

    def parameters(self) -> list[Parameter]:
        return [Parameter(name="npc", type=str, required=True)]

    def run_command(self, line: str, session_id: uuid.UUID) -> str:
        session = self._sessions.get(session_id)
        room = self._world.get_room(session.player.current_room_id)

        query = command_argument(line, self.name())
        if query.lower().startswith("to "):
            query = query[3:]
        query = query.strip().lower()
        if not query:
            return "Talk to whom?"

        npc = next(
            (n for n in room.npcs if query in n.id.lower() or query in n.name.lower()),
            None,
        )
        if npc is None:
            return f"You don't see {query!r} here."

        try:
            if self._llm_client is None:
                self._llm_client = self._get_llm_client()
            reply = self._llm_client.complete(
                _GREETING_PROMPT,
                system=(
                    f"You are {npc.name}, an NPC in a text-based multiplayer "
                    f"dungeon game. {npc.description}"
                ).strip(),
            )
        except (LLMConfigError, LLMRequestError) as exc:
            logger.warning("talk to %r failed: %s", npc.name, exc)
            return f"{npc.name} doesn't seem to hear you."

        return f"{npc.name} says: {reply.strip()}"
