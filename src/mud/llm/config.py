"""LLM provider selection: environment variables first, then a config
file, so settings don't have to live in the shell environment if you'd
rather not.

Precedence (highest wins): an explicit constructor argument (if you
build a *_llm_client.py class directly) > environment variable >
CONFIG_FILE > each provider's own built-in default.

    MUD_LLM_PROVIDER   which LLMClient implementation get_llm_client()
                       builds. Adding a new provider later means adding
                       a branch here and a new *_llm_client.py, not
                       touching callers.
    MUD_LLM_API_KEY    the selected provider's API key. Deliberately
                       generic rather than e.g. ANTHROPIC_API_KEY -- the
                       point of going through a provider name is to stay
                       able to switch providers without renaming env
                       vars (or config file keys) too.
    MUD_LLM_MODEL      optional; each provider has its own default (see
                       that provider's *_llm_client.py).

CONFIG_FILE (llm.config.json, in the project root) holds the same three
settings as a JSON object, for whichever of them you'd rather keep out
of the environment:

    {
      "provider": "anthropic",
      "api_key": null,
      "model": null
    }

The file is optional -- missing entirely is treated the same as every
field being null -- and any field left null/omitted falls through to
the matching environment variable, then the built-in default. It's
gitignored, since api_key is a secret once you fill it in.

`setting()` below is the shared lookup each provider's client uses for
its own keys (api_key, model, ...): env var, then config file, then
None. get_llm_client() uses it for "provider" and leaves the rest to
whichever client it builds, so each provider owns resolving (and
defaulting) its own settings -- that client is imported only once its
provider is actually selected, so picking "anthropic" doesn't require
some other provider's SDK to be installed too.
"""

from __future__ import annotations

import json
import os
from pathlib import Path

from mud.llm.llm_client import LLMClient

DEFAULT_PROVIDER = "anthropic"

# This file lives at src/mud/llm/config.py, so the project root (where
# the config file lives, alongside README.md/requirements.txt) is three
# levels up.
CONFIG_FILE = Path(__file__).resolve().parents[3] / "llm.config.json"


class LLMConfigError(Exception):
    """Raised when LLM configuration is missing or invalid (an unknown
    provider, a missing API key, a malformed config file, etc)."""


def load_config_file() -> dict:
    """Read CONFIG_FILE into a dict, or {} if it doesn't exist. Raises
    LLMConfigError if it exists but isn't a valid JSON object."""
    if not CONFIG_FILE.is_file():
        return {}

    try:
        data = json.loads(CONFIG_FILE.read_text())
    except json.JSONDecodeError as e:
        raise LLMConfigError(f"{CONFIG_FILE}: invalid JSON ({e})") from e

    if not isinstance(data, dict):
        raise LLMConfigError(
            f"{CONFIG_FILE}: expected a JSON object, got {type(data).__name__}"
        )
    return data


def setting(env_var: str, file_key: str) -> str | None:
    """One setting's value: the environment variable if set, else
    CONFIG_FILE's value (treating null/missing as unset), else None --
    the caller applies whatever default makes sense for that setting."""
    value = os.environ.get(env_var)
    if value:
        return value
    return load_config_file().get(file_key) or None


def get_llm_client() -> LLMClient:
    """Build the LLMClient selected by config (see module docstring for
    where each setting can come from)."""
    provider = (setting("MUD_LLM_PROVIDER", "provider") or DEFAULT_PROVIDER)
    provider = provider.strip().lower()

    if provider == "anthropic":
        from mud.llm.anthropic_llm_client import AnthropicLLMClient

        return AnthropicLLMClient()

    raise LLMConfigError(
        f"Unknown MUD_LLM_PROVIDER: {provider!r} (expected: 'anthropic')"
    )
