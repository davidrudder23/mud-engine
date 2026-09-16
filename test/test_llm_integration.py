"""Integration test: an actual network call to whichever LLM provider
is configured, to confirm real end-to-end connectivity -- network, API
key, model name all correct together -- rather than just the client's
own request/response-handling logic (see test_llm.py for that, which is
entirely offline/faked and always runs).

Automatically skipped if no API key is configured anywhere config.py
looks (neither MUD_LLM_API_KEY nor "api_key" in llm.config.json): fill
in either to exercise this for real. When it does run, it makes one
real request to the provider and costs a small number of tokens.
"""

from __future__ import annotations

import unittest

from mud.llm.config import LLMConfigError, get_llm_client, setting
from mud.llm.llm_client import LLMRequestError


def _has_api_key() -> bool:
    return bool(setting("MUD_LLM_API_KEY", "api_key"))


@unittest.skipUnless(
    _has_api_key(),
    "No LLM API key configured -- set MUD_LLM_API_KEY or \"api_key\" in "
    "llm.config.json to run this test.",
)
class LLMConnectionTests(unittest.TestCase):
    def test_can_reach_the_configured_llm(self) -> None:
        try:
            client = get_llm_client()
            reply = client.complete(
                "Reply with exactly one word: PONG",
                system=(
                    "You are being used to test API connectivity. Follow "
                    "the instruction exactly -- reply with nothing else."
                ),
            )
        except (LLMConfigError, LLMRequestError) as e:
            self.fail(
                f"Could not reach the configured LLM ({type(e).__name__}): {e}"
            )

        self.assertIn("PONG", reply.upper())


if __name__ == "__main__":
    unittest.main()
