"""Session: one connection's state for the telnet server.

A session is tied to a Player (see the login step in telnet_server),
not to a room directly — location lives on the player
(`player.current_room_id`), since it's the player's location that
persists, not the connection's. This is one-directional: a session
knows its player (`session.player`), but a Player doesn't know which
session(s) it's in — the same player can be behind multiple sessions at
once (see mud/player.py on why that's supported deliberately).
"""

from __future__ import annotations

import uuid
from dataclasses import dataclass, field

from mud.player import Player


@dataclass
class Session:
    player: Player
    id: uuid.UUID = field(default_factory=uuid.uuid4)
    # Set by QuitCommand; telnet_server's read loop checks this after
    # every command and closes the connection if it's set.
    should_close: bool = False


class SessionRegistry:
    """Tracks live sessions, keyed by session id."""

    def __init__(self) -> None:
        self._sessions: dict[uuid.UUID, Session] = {}

    def create(self, player: Player) -> Session:
        session = Session(player=player)
        self._sessions[session.id] = session
        return session

    def get(self, session_id: uuid.UUID) -> Session:
        try:
            return self._sessions[session_id]
        except KeyError:
            raise KeyError(f"No such session: {session_id!r}") from None

    def remove(self, session_id: uuid.UUID) -> None:
        self._sessions.pop(session_id, None)
