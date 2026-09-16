"""Command: something a player can type and have the system act on.

Per command_structure.md: a command is an object that knows whether a
line of input is meant for it (`match_command`, a check only — it
doesn't run anything), and how to actually run it (`run_command`).

The spec also talks about an `execute` function taking a session id "so
we can tie commands together into a conversation", separately from
`match_command`/`run_command` taking the input string. Rather than have
three methods with two of them doing the same job, `run_command` takes
both: the string (to parse arguments from) and the session id (so a
command can read/change state tied to that connection, via whatever it
needs — e.g. `look` needs the SessionRegistry to know which room to
describe).

`match_room` is a second, separate match: "is this command usable in
`room`?", as opposed to `match_command`'s "does this line invoke it?".
It exists for `help` (see commands/help.py) to list only the commands
that apply where the player currently stands. For now every command
returns True unconditionally — nothing yet restricts a command to
certain rooms — so this is a stub until that becomes real.

`parameters()` -- together with `name()` and `describe()` -- is what
moves command matching off of `matches_verb`'s plain prefix check and
onto an LLM (see commands/llm_runner.py): before a line can be handed
to a model and matched to a command with its arguments extracted, each
command has to say, explicitly, what it's called, what it does, and
what arguments it takes -- a name, a type, and whether it's required.
`run_command` itself is unchanged by this -- it still parses `line` by
hand with `command_argument`/`matches_verb`. LLMCommandRunner works
around that rather than through it: once it has a command name and
parameter values from the model, it reconstructs a normalized line
(e.g. "go north") and calls the matched command's own `run_command`
with that, so every command's existing argument parsing keeps working
unmodified either way.
"""

from __future__ import annotations

import uuid
from abc import ABC, abstractmethod
from dataclasses import dataclass

from mud.room import Room


@dataclass(frozen=True)
class Parameter:
    """One argument a command expects, beyond the verb itself.

    `type` is the Python type the argument should be parsed/validated
    as (every command's arguments are `str` today -- nothing takes a
    number or an enum yet, but this doesn't assume that stays true).
    `required=False` means the command still runs without it (nothing
    currently uses this; every existing command's arguments are
    required, per each command's own "X what?" prompt when it's
    missing).
    """

    name: str
    type: type
    required: bool = True


class Command(ABC):
    @abstractmethod
    def name(self) -> str:
        """This command's canonical verb, e.g. "go" or "pick up". Used
        both for matching (matches_verb(line, self.name())) and as the
        stable identifier LLMCommandRunner matches the model's reply
        against -- unlike `describe()`, meant to be read by a player,
        this is meant to be matched by code."""

    @abstractmethod
    def match_command(self, line: str) -> bool:
        """Return whether this command handles `line`. Must not run it."""

    @abstractmethod
    def match_room(self, room: Room) -> bool:
        """Return whether this command is usable in `room`. Stub: always
        True for now (see module docstring)."""

    @abstractmethod
    def describe(self) -> str:
        """One-line, player-facing description for the `help` listing."""

    @abstractmethod
    def parameters(self) -> list[Parameter]:
        """The arguments this command expects, beyond the verb itself.
        Empty for a command that takes none (e.g. `look`, `quit`)."""

    @abstractmethod
    def run_command(self, line: str, session_id: uuid.UUID) -> str:
        """Run this command and return the text to show the player."""


def matches_verb(line: str, verb: str) -> bool:
    """Case-insensitive check that `line` starts with `verb`.

    Commands can take parameters ("go east", "look around"), so matching
    is a prefix check against the verb, not equality against the whole
    line. `run_command` is responsible for anything after the verb, if
    it cares.

    Note this is a plain prefix check, not a word-boundary one: "goblin"
    matches verb "go". That's intentional per the current spec (a
    stricter word-boundary match was tried and reverted).
    """
    return line.strip().lower().startswith(verb.lower())


def command_argument(line: str, verb: str) -> str:
    """Whatever follows `verb` in `line`, stripped. Assumes
    `matches_verb(line, verb)` is already true.

    Unlike a plain `line.split(maxsplit=1)` (what `go` uses), this works
    for verbs with an embedded space, e.g. "pick up sword" against verb
    "pick up" -- splitting on whitespace would cut after "pick", not
    after "pick up".
    """
    return line.strip()[len(verb) :].strip()
