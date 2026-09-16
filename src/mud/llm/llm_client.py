"""Abstract interface for calling an LLM.

Mirrors PlayerRepository (mud/repositories/player_repository.py): game
logic should depend on this interface, not on a specific provider's SDK,
so the backend can change (a different model, a different vendor
entirely) without touching anything that uses an LLMClient.

The eventual use case is matching a player's typed line to a command and
extracting its parameters, but nothing here is shaped around that yet --
this is deliberately just "send text, get text back" until there's a
concrete prompt/response contract to design around.
"""

from __future__ import annotations

from abc import ABC, abstractmethod


class LLMClient(ABC):
    @abstractmethod
    def complete(self, prompt: str, *, system: str | None = None) -> str:
        """Send `prompt` to the model and return its text reply.

        `system` is an optional system prompt (steers the model's
        behavior generally, separate from `prompt` itself). Raises
        LLMRequestError if the call fails (bad/missing API key, network
        error, provider-side error, etc).
        """


class LLMRequestError(Exception):
    """Raised when a call to the underlying LLM fails."""
