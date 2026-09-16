"""Claude implementation of LLMClient, via Anthropic's Python SDK."""

from __future__ import annotations

import anthropic

from mud.llm.config import CONFIG_FILE, LLMConfigError, setting
from mud.llm.llm_client import LLMClient, LLMRequestError

# Per-turn look-ups over one typed line are a small, well-defined task,
# so the cheaper/faster end of the current model lineup is the sensible
# default; override with MUD_LLM_MODEL for a larger model if needed.
DEFAULT_MODEL = "claude-sonnet-5"
DEFAULT_MAX_TOKENS = 1024


class AnthropicLLMClient(LLMClient):
    def __init__(
        self,
        *,
        api_key: str | None = None,
        model: str | None = None,
        client: anthropic.Anthropic | None = None,
    ) -> None:
        """`client` lets tests inject a fake in place of the real SDK
        client; every other caller should leave it unset."""
        api_key = api_key or setting("MUD_LLM_API_KEY", "api_key")
        if not api_key and client is None:
            raise LLMConfigError(
                "No Anthropic API key found: set MUD_LLM_API_KEY, or "
                f"\"api_key\" in {CONFIG_FILE} (see mud/llm/config.py)"
            )

        self._model = model or setting("MUD_LLM_MODEL", "model") or DEFAULT_MODEL
        self._client = client if client is not None else anthropic.Anthropic(api_key=api_key)

    def complete(self, prompt: str, *, system: str | None = None) -> str:
        kwargs: dict = {}
        if system:
            kwargs["system"] = system

        try:
            response = self._client.messages.create(
                model=self._model,
                max_tokens=DEFAULT_MAX_TOKENS,
                messages=[{"role": "user", "content": prompt}],
                **kwargs,
            )
        except anthropic.AnthropicError as e:
            raise LLMRequestError(f"Anthropic request failed: {e}") from e

        return "".join(
            block.text for block in response.content if block.type == "text"
        )
