"""Claude implementation of LLMClient, via Anthropic's Python SDK."""

from __future__ import annotations

import logging
import time

import anthropic

from mud.llm.config import CONFIG_FILE, LLMConfigError, setting
from mud.llm.llm_client import LLMClient, LLMRequestError

# Per-turn look-ups over one typed line are a small, well-defined task,
# so the cheaper/faster end of the current model lineup is the sensible
# default; override with MUD_LLM_MODEL for a larger model if needed.
DEFAULT_MODEL = "claude-sonnet-5"
DEFAULT_MAX_TOKENS = 1024

logger = logging.getLogger(__name__)


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

        started = time.monotonic()
        try:
            response = self._client.messages.create(
                model=self._model,
                max_tokens=DEFAULT_MAX_TOKENS,
                messages=[{"role": "user", "content": prompt}],
                **kwargs,
            )
        except anthropic.AnthropicError as e:
            logger.warning(
                "LLM call failed: model=%s elapsed_ms=%.0f error=%s",
                self._model, (time.monotonic() - started) * 1000, e,
            )
            raise LLMRequestError(f"Anthropic request failed: {e}") from e

        # `usage` is present on every real Anthropic response, but this
        # is read defensively (getattr, not response.usage.input_tokens
        # directly) in case a future SDK response or a test double omits
        # it -- "if possible" per the ask, not "must always be there".
        usage = getattr(response, "usage", None)
        input_tokens = getattr(usage, "input_tokens", None)
        output_tokens = getattr(usage, "output_tokens", None)
        logger.info(
            "LLM call: model=%s input_tokens=%s output_tokens=%s elapsed_ms=%.0f",
            self._model, input_tokens, output_tokens, (time.monotonic() - started) * 1000,
        )

        return "".join(
            block.text for block in response.content if block.type == "text"
        )
