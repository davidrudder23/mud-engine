"""Test doubles shared across the suite.

FakePlayerRepository stands in for PostgresPlayerRepository so command
and telnet_server tests don't need a real database. It deliberately
mirrors PostgresPlayerRepository's actual behavior rather than the
simplest thing that would pass: every get_by_id/get_by_name/create call
builds a *fresh* Player instance from stored data (like a fresh row
fetch would), instead of handing back a cached object. That distinction
matters here -- Player.eq is disabled specifically so that two logins
under the same name are two independent instances (see mud/player.py
and the telnet_server module docstring) -- so a fake that cached and
reused instances would let a bug in that area pass unnoticed.

FakeStreamReader/FakeStreamWriter stand in for the asyncio streams
telnet_server talks to, so _handle_client can be driven end-to-end
without opening a real socket.

FakeAnthropicClient stands in for anthropic.Anthropic itself (injected
via AnthropicLLMClient's `client=` parameter), so mud.llm tests never
make a real network call or need a real API key.

FakeLLMClient is a level higher: it implements LLMClient itself, for
tests of code that *calls* complete() (LLMCommandRunner) rather than
tests of complete()'s own implementation (that's what
FakeAnthropicClient/test_llm.py are for).
"""

from __future__ import annotations

import uuid
from datetime import datetime, timezone
from types import SimpleNamespace

from mud.llm.llm_client import LLMClient
from mud.player import Player
from mud.repositories.player_repository import DuplicatePlayerName, PlayerRepository


class FakePlayerRepository(PlayerRepository):
    def __init__(self) -> None:
        self._rows: dict[uuid.UUID, dict] = {}

    def create(self, name: str, starting_room_id: str) -> Player:
        if self.get_by_name(name) is not None:
            raise DuplicatePlayerName(f"Player name already taken: {name!r}")
        now = datetime.now(timezone.utc)
        player_id = uuid.uuid4()
        self._rows[player_id] = {
            "name": name,
            "current_room_id": starting_room_id,
            "created_at": now,
            "updated_at": now,
        }
        return self._row_to_player(player_id)

    def get_by_id(self, player_id: uuid.UUID) -> Player | None:
        return self._row_to_player(player_id) if player_id in self._rows else None

    def get_by_name(self, name: str) -> Player | None:
        for player_id, row in self._rows.items():
            if row["name"] == name:
                return self._row_to_player(player_id)
        return None

    def save(self, player: Player) -> None:
        if player.id not in self._rows:
            raise ValueError(f"Cannot save unknown player: {player.id!r}")
        row = self._rows[player.id]
        row["current_room_id"] = player.current_room_id
        row["updated_at"] = datetime.now(timezone.utc)

    def list_all(self) -> list[Player]:
        return [self._row_to_player(player_id) for player_id in self._rows]

    def _row_to_player(self, player_id: uuid.UUID) -> Player:
        row = self._rows[player_id]
        return Player(
            id=player_id,
            name=row["name"],
            current_room_id=row["current_room_id"],
            created_at=row["created_at"],
            updated_at=row["updated_at"],
        )


class FakeStreamReader:
    """Yields pre-scripted lines to `await reader.readline()` calls, then
    behaves like a closed connection (returns b"") once exhausted --
    unless `raise_after` is set, in which case it raises that exception
    instead of returning EOF, to exercise telnet_server's error handling.
    """

    def __init__(self, lines: list[bytes], *, raise_after: Exception | None = None) -> None:
        self._lines = list(lines)
        self._raise_after = raise_after

    async def readline(self) -> bytes:
        if self._lines:
            return self._lines.pop(0)
        if self._raise_after is not None:
            exc, self._raise_after = self._raise_after, None
            raise exc
        return b""


class FakeStreamWriter:
    """Collects everything written to it so a test can assert on the
    transcript, and records close()/wait_closed() calls."""

    def __init__(self, peer: tuple[str, int] = ("127.0.0.1", 12345)) -> None:
        self._peer = peer
        self._buffer = bytearray()
        self.closed = False
        self.wait_closed_called = False

    def write(self, data: bytes) -> None:
        if self.closed:
            raise ConnectionError("write() after close()")
        self._buffer.extend(data)

    async def drain(self) -> None:
        pass

    def get_extra_info(self, name: str):
        return self._peer if name == "peername" else None

    def close(self) -> None:
        self.closed = True

    async def wait_closed(self) -> None:
        self.wait_closed_called = True

    def text(self) -> str:
        return self._buffer.decode()


class FakeAnthropicClient:
    """Stands in for anthropic.Anthropic: AnthropicLLMClient calls
    `self._client.messages.create(...)`, so `messages` is `self` here --
    `.create` is the one method that matters, and aliasing `messages` to
    the same object means a test can override `fake.create` directly
    (see test_llm.py's multi-text-block test) and have it take effect
    through `fake.messages.create` too.

    Records every call's kwargs in `.calls`, for asserting on what
    AnthropicLLMClient.complete() actually sent. Returns a `reply` as a
    single text content block by default, or raises `error` if one was
    given (to exercise the LLMRequestError-wrapping path).
    """

    def __init__(self, *, reply: str = "", error: Exception | None = None) -> None:
        self.reply = reply
        self.error = error
        self.calls: list[dict] = []
        self.messages = self

    def create(self, **kwargs):
        self.calls.append(kwargs)
        if self.error is not None:
            raise self.error
        return SimpleNamespace(
            content=[SimpleNamespace(type="text", text=self.reply)]
        )


class FakeLLMClient(LLMClient):
    """A scripted LLMClient: `reply` is either a fixed string returned
    for every call, or a callable `(prompt, system) -> str` for tests
    that need the reply to depend on the input. Raises `error` instead,
    if given, to exercise callers' error handling. Records every call's
    (prompt, system) in `.calls`.
    """

    def __init__(self, reply: str | object = "", *, error: Exception | None = None) -> None:
        self.reply = reply
        self.error = error
        self.calls: list[dict] = []

    def complete(self, prompt: str, *, system: str | None = None) -> str:
        self.calls.append({"prompt": prompt, "system": system})
        if self.error is not None:
            raise self.error
        if callable(self.reply):
            return self.reply(prompt, system)
        return self.reply
