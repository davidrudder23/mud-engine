"""Tests for the LLM client setup: the AnthropicLLMClient implementation
and the MUD_LLM_PROVIDER-based factory + config file in mud.llm.config.

Nothing here makes a real network call -- AnthropicLLMClient accepts an
injected `client=` in place of the real anthropic.Anthropic, and
test.fakes.FakeAnthropicClient stands in for it (see fakes.py). No
MUD_LLM_API_KEY, and no llm.config.json, is needed to run this suite:
every test here points mud.llm.config.CONFIG_FILE at a scratch path
(see IsolatedConfigTestCase) so it's never affected by -- or able to
affect -- the real config file in the project root.
"""

from __future__ import annotations

import json
import os
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import anthropic

from mud.llm import config
from mud.llm.anthropic_llm_client import DEFAULT_MODEL, AnthropicLLMClient
from mud.llm.config import LLMConfigError, get_llm_client
from mud.llm.llm_client import LLMRequestError

from test.fakes import FakeAnthropicClient


class IsolatedConfigTestCase(unittest.TestCase):
    """Points mud.llm.config.CONFIG_FILE at a scratch path (that doesn't
    exist until a test calls write_config_file) for the duration of each
    test, so nothing here reads -- or writes -- the real llm.config.json
    in the project root."""

    def setUp(self) -> None:
        tmpdir = tempfile.TemporaryDirectory()
        self.addCleanup(tmpdir.cleanup)
        self.config_path = Path(tmpdir.name) / "llm.config.json"

        patcher = patch.object(config, "CONFIG_FILE", self.config_path)
        patcher.start()
        self.addCleanup(patcher.stop)

    def write_config_file(self, data) -> None:
        self.config_path.write_text(json.dumps(data))


class AnthropicLLMClientConstructionTests(IsolatedConfigTestCase):
    def test_missing_api_key_and_no_injected_client_raises(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(LLMConfigError):
                AnthropicLLMClient()

    def test_injected_client_does_not_require_an_api_key(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            AnthropicLLMClient(client=FakeAnthropicClient())  # must not raise

    def test_explicit_api_key_argument_is_accepted(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            AnthropicLLMClient(api_key="sk-explicit")  # must not raise

    def test_default_model(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            client = AnthropicLLMClient(client=FakeAnthropicClient())
        self.assertEqual(client._model, DEFAULT_MODEL)

    def test_model_argument_overrides_default(self) -> None:
        client = AnthropicLLMClient(client=FakeAnthropicClient(), model="claude-haiku-4-5")
        self.assertEqual(client._model, "claude-haiku-4-5")

    def test_model_env_var_overrides_default(self) -> None:
        with patch.dict(os.environ, {"MUD_LLM_MODEL": "claude-opus-5"}):
            client = AnthropicLLMClient(client=FakeAnthropicClient())
        self.assertEqual(client._model, "claude-opus-5")


class ConfigFileTests(IsolatedConfigTestCase):
    """The config-file half of mud.llm.config: reading llm.config.json,
    and its precedence against environment variables."""

    def test_missing_file_is_not_an_error(self) -> None:
        self.assertEqual(config.load_config_file(), {})

    def test_malformed_json_raises_llm_config_error(self) -> None:
        self.config_path.write_text("{not json")
        with self.assertRaises(LLMConfigError):
            config.load_config_file()

    def test_non_object_json_raises_llm_config_error(self) -> None:
        self.config_path.write_text("[1, 2, 3]")
        with self.assertRaises(LLMConfigError):
            config.load_config_file()

    def test_api_key_is_read_from_config_file(self) -> None:
        self.write_config_file({"api_key": "sk-from-file"})
        with patch.dict(os.environ, {}, clear=True):
            client = AnthropicLLMClient()  # must not raise -- key came from the file
        self.assertIsInstance(client, AnthropicLLMClient)

    def test_model_is_read_from_config_file(self) -> None:
        self.write_config_file({"model": "claude-opus-5"})
        with patch.dict(os.environ, {}, clear=True):
            client = AnthropicLLMClient(client=FakeAnthropicClient())
        self.assertEqual(client._model, "claude-opus-5")

    def test_null_fields_fall_through_to_env_var(self) -> None:
        self.write_config_file({"api_key": None, "model": None})
        with patch.dict(os.environ, {"MUD_LLM_MODEL": "claude-opus-5"}):
            client = AnthropicLLMClient(client=FakeAnthropicClient())
        self.assertEqual(client._model, "claude-opus-5")

    def test_env_var_takes_precedence_over_config_file(self) -> None:
        self.write_config_file({"model": "claude-from-file"})
        with patch.dict(os.environ, {"MUD_LLM_MODEL": "claude-from-env"}):
            client = AnthropicLLMClient(client=FakeAnthropicClient())
        self.assertEqual(client._model, "claude-from-env")

    def test_provider_is_read_from_config_file(self) -> None:
        self.write_config_file({"provider": "anthropic", "api_key": "sk-from-file"})
        with patch.dict(os.environ, {}, clear=True):
            client = get_llm_client()
        self.assertIsInstance(client, AnthropicLLMClient)

    def test_env_provider_overrides_config_file_provider(self) -> None:
        self.write_config_file({"provider": "some-other-llm"})
        env = {"MUD_LLM_PROVIDER": "anthropic", "MUD_LLM_API_KEY": "sk-test"}
        with patch.dict(os.environ, env, clear=True):
            client = get_llm_client()
        self.assertIsInstance(client, AnthropicLLMClient)


class AnthropicLLMClientCompleteTests(IsolatedConfigTestCase):
    def test_sends_prompt_as_a_user_message_and_returns_reply_text(self) -> None:
        fake = FakeAnthropicClient(reply="a torch-lit room")
        client = AnthropicLLMClient(client=fake)

        result = client.complete("describe this room")

        self.assertEqual(result, "a torch-lit room")
        self.assertEqual(len(fake.calls), 1)
        call = fake.calls[0]
        self.assertEqual(call["messages"], [{"role": "user", "content": "describe this room"}])
        self.assertEqual(call["model"], DEFAULT_MODEL)
        self.assertNotIn("system", call)  # none given -> not sent at all

    def test_system_prompt_is_passed_through_when_given(self) -> None:
        fake = FakeAnthropicClient(reply="ok")
        client = AnthropicLLMClient(client=fake)

        client.complete("go north", system="You are a command parser.")

        self.assertEqual(fake.calls[0]["system"], "You are a command parser.")

    def test_concatenates_multiple_text_blocks(self) -> None:
        # A real response can carry more than one text block (e.g.
        # interleaved with other block types in the future); complete()
        # must join all of them, not just return the first.
        fake = FakeAnthropicClient()
        fake.create = lambda **kwargs: SimpleNamespace(
            content=[
                SimpleNamespace(type="text", text="hello "),
                SimpleNamespace(type="text", text="world"),
            ]
        )
        client = AnthropicLLMClient(client=fake)

        self.assertEqual(client.complete("hi"), "hello world")

    def test_provider_error_is_wrapped_in_llm_request_error(self) -> None:
        fake = FakeAnthropicClient(error=anthropic.AnthropicError("rate limited"))
        client = AnthropicLLMClient(client=fake)

        with self.assertRaises(LLMRequestError):
            client.complete("go north")


class GetLLMClientTests(IsolatedConfigTestCase):
    def test_default_provider_is_anthropic(self) -> None:
        with patch.dict(os.environ, {"MUD_LLM_API_KEY": "sk-test"}, clear=True):
            client = get_llm_client()
        self.assertIsInstance(client, AnthropicLLMClient)

    def test_provider_name_is_case_and_whitespace_insensitive(self) -> None:
        env = {"MUD_LLM_PROVIDER": "  Anthropic  ", "MUD_LLM_API_KEY": "sk-test"}
        with patch.dict(os.environ, env, clear=True):
            client = get_llm_client()
        self.assertIsInstance(client, AnthropicLLMClient)

    def test_unknown_provider_raises_llm_config_error(self) -> None:
        env = {"MUD_LLM_PROVIDER": "some-other-llm", "MUD_LLM_API_KEY": "sk-test"}
        with patch.dict(os.environ, env, clear=True):
            with self.assertRaises(LLMConfigError):
                get_llm_client()

    def test_anthropic_provider_without_api_key_raises_llm_config_error(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(LLMConfigError):
                get_llm_client()


if __name__ == "__main__":
    unittest.main()
