"""LLMCommandRunner: matches a line of input to a command using an
LLMClient, instead of CommandRunner's plain matches_verb prefix check.

Same public shape as CommandRunner (`run(line, session_id) -> str`), so
telnet_server can use either one interchangeably.

Each command already declares what it's called (`name()`), what it does
(`describe()`), and what arguments it takes (`parameters()`) -- see
command.py. This runner turns that into a system prompt, asks the LLM
which command (if any) a player's line invokes and what values to use
for its parameters, then hands off to that command's own run_command.

Deliberately *not* a rewrite of each command's argument parsing: the
model's extracted parameters are turned into a normalized line (e.g.
"go north") via command.build_line() (see command.py), which is then
handed to the matched command's own run_command exactly as
CommandRunner would -- so `pick up`'s substring
item matching, `go`'s "Go where?" prompt, etc. all keep working
unmodified, regardless of which runner is dispatching to them. This
also means a missing required parameter isn't validated here at all --
the underlying command already prompts for it once handed a shorter
line (e.g. "go" alone -> "Go where?"), so there's nothing extra to do.

Failure modes (LLMRequestError, malformed/unparseable JSON, a
hallucinated command name not in the registered list, or "command":
null) all fall back to the same "Unknown command" reply CommandRunner
gives for a line nothing matches -- from the player's side, a line the
model couldn't place looks the same as one no verb recognizes.
"""

from __future__ import annotations

import json
import logging
import re
import uuid

from mud.commands.command import Command
from mud.llm.llm_client import LLMClient, LLMRequestError

logger = logging.getLogger(__name__)

_INSTRUCTIONS = """\
You are the command parser for a text-based multi-user dungeon (MUD) game.
A player just typed a line of text. Decide which ONE of the commands listed
below it invokes, and extract that command's parameter values from the text.

Commands:
{command_list}

Respond with ONLY a JSON object (no markdown fences, no other text), in
exactly this shape:
{{"command": "<one of the command names above, or null if none apply>", "parameters": {{"<name>": "<value>", ...}}}}

Only include a parameter in "parameters" if its value is actually present
in the player's text -- never invent or guess a value. If a required
parameter is missing, still return the best-matching command name; the
game will ask the player for it. If no command plausibly applies, set
"command" to null.\
"""


def _describe_parameters(command: Command) -> str:
    params = command.parameters()
    if not params:
        return "none"
    return ", ".join(
        f"{p.name} ({p.type.__name__}, {'required' if p.required else 'optional'})"
        for p in params
    )


def _describe_for_prompt(command: Command) -> str:
    # describe()'s player-facing text is column-padded for the `help`
    # listing (e.g. "go <dir>    moves..."); collapse that whitespace so
    # it doesn't look broken inside a prompt.
    return " ".join(command.describe().split())


def build_system_prompt(commands: list[Command]) -> str:
    command_list = "\n".join(
        f'- "{command.name()}": {_describe_for_prompt(command)} '
        f"Parameters: {_describe_parameters(command)}."
        for command in commands
    )
    return _INSTRUCTIONS.format(command_list=command_list)


def _extract_json_object(reply: str) -> dict | None:
    """Best-effort parse of the model's reply into a dict, stripping a
    markdown code fence if present (models sometimes wrap JSON in one
    despite being told not to). None if it's not parseable JSON, or
    parses to something other than a JSON object."""
    text = reply.strip()

    fence = re.match(r"^```(?:json)?\s*(.*?)\s*```$", text, re.DOTALL)
    if fence:
        text = fence.group(1)

    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return None

    return data if isinstance(data, dict) else None


class LLMCommandRunner:
    def __init__(self, llm_client: LLMClient, commands: list[Command]) -> None:
        self._llm_client = llm_client
        self._commands = commands
        self._by_name = {command.name(): command for command in commands}
        self._system_prompt = build_system_prompt(commands)

    def run(self, line: str, session_id: uuid.UUID) -> str:
        try:
            reply = self._llm_client.complete(line, system=self._system_prompt)
        except LLMRequestError as exc:
            logger.warning("LLM request failed for line %r: %s", line, exc)
            return f"Unknown command: {line.strip()!r}"

        data = _extract_json_object(reply)
        if data is None:
            logger.warning("Could not parse LLM reply for line %r: %r", line, reply)
            return f"Unknown command: {line.strip()!r}"

        name = data.get("command")
        parameters = data.get("parameters")
        if not isinstance(parameters, dict):
            parameters = {}

        if not isinstance(name, str) or not name:
            # A well-formed, deliberate "no command applies" -- not a
            # parsing failure, so this doesn't warrant a warning.
            logger.info("LLM found no matching command for line %r", line)
            return f"Unknown command: {line.strip()!r}"

        command = self._by_name.get(name)
        if command is None:
            logger.info("LLM matched line %r to unregistered command %r", line, name)
            return f"Unknown command: {line.strip()!r}"

        normalized_line = command.build_line(parameters)
        logger.debug("LLM matched line %r to %r parameters=%r", line, name, parameters)
        return command.run_command(normalized_line, session_id)
