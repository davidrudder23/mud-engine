// Package commands holds everything a player can type: the Command
// interface, one implementation per verb, and the two runners that
// dispatch a line to one (plain verb-prefix matching, or an LLM).
//
// A command knows whether a line of input is meant for it
// (MatchCommand -- a check only, it doesn't run anything) and how to
// actually run it (RunCommand, which takes both the line, to parse
// arguments from, and the session id, so it can read/change state tied
// to that connection).
//
// A command answers to one or more aliases (see verbs): "go" also
// answers to "leave", "exit", and "travel". The first alias is the
// command's canonical Name; matching accepts any of them.
//
// MatchRoom is a second, separate match: "is this command usable in
// room?", for `help` to list only commands that apply where the player
// stands. Every command returns true unconditionally for now -- nothing
// yet restricts a command to certain rooms.
//
// Name, Describe, and Parameters are what let command matching move
// onto an LLM (see LLMCommandRunner) and be exposed over MCP: each
// command says explicitly what it's called, what it does, and what
// arguments it takes. RunCommand itself still parses the line by hand
// with CommandArgument/MatchesVerb; anything that gets a command name
// plus parameter values from somewhere other than a typed line (a
// model's JSON reply, an MCP tool call) uses BuildLine to reconstruct a
// normalized line (e.g. "go north") and hands that to RunCommand, so
// every command's own argument parsing keeps working unmodified.
package commands

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/world"
)

// ParamType is the type a command argument should be parsed/validated
// as. Every argument is a string today, but this doesn't assume that
// stays true.
type ParamType string

const String ParamType = "string"

// Parameter is one argument a command expects, beyond the verb itself.
// Required=false means the command still runs without it (nothing
// uses that yet).
type Parameter struct {
	Name     string
	Type     ParamType
	Required bool
}

type Command interface {
	// Name is this command's canonical verb, e.g. "go" or "pick up".
	// Used both for matching and as the stable identifier the LLM
	// runner and MCP server key on -- meant to be matched by code,
	// unlike Describe.
	Name() string
	// Aliases are every verb this command answers to, Name first.
	Aliases() []string
	// MatchCommand reports whether this command handles line (whether
	// it starts with any alias). Must not run it.
	MatchCommand(line string) bool
	// MatchRoom reports whether this command is usable in room.
	MatchRoom(room *world.Room) bool
	// Describe is a one-line, player-facing description for `help`.
	Describe() string
	// Parameters are the arguments this command expects, beyond the
	// verb itself. Empty for a command that takes none.
	Parameters() []Parameter
	// RunCommand runs this command and returns the text to show the
	// player. An error means something went wrong beyond anything the
	// player typed (a missing session, a failed save).
	RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error)
}

// BuildLine rebuilds a line c.RunCommand can parse itself, from
// parameter values keyed by name (e.g. {"direction": "north"} -> "go
// north" for go). A parameter missing (or empty) in the map is just
// omitted -- RunCommand already prompts for what's missing -- and
// undeclared keys are ignored.
func BuildLine(c Command, parameters map[string]string) string {
	parts := []string{c.Name()}
	for _, p := range c.Parameters() {
		if value := parameters[p.Name]; value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
}

// MatchesVerb is a case-insensitive check that line starts with verb.
//
// Commands can take parameters ("go east"), so matching is a prefix
// check against the verb, not equality against the whole line. This is
// a plain prefix check, not a word-boundary one: "goblin" matches verb
// "go". That's intentional per the current spec.
func MatchesVerb(line, verb string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), strings.ToLower(verb))
}

// CommandArgument returns whatever follows verb in line, trimmed.
// Assumes MatchesVerb(line, verb) is already true. Unlike splitting on
// the first space (what `go` does), this works for verbs with an
// embedded space, e.g. "pick up sword" against verb "pick up".
func CommandArgument(line, verb string) string {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < len(verb) {
		return ""
	}
	return strings.TrimSpace(trimmed[len(verb):])
}

// MatchAlias returns the alias line starts with (per MatchesVerb),
// preferring the longest when several do -- so an alias like "pick up"
// wins over "pick" and the argument after it is split off correctly.
func MatchAlias(line string, aliases []string) (string, bool) {
	best, found := "", false
	for _, alias := range aliases {
		if MatchesVerb(line, alias) && len(alias) > len(best) {
			best, found = alias, true
		}
	}
	return best, found
}

// verbs is a command's aliases, canonical name first. Embedding it
// gives a command its Name, Aliases, and MatchCommand; to add an alias,
// add it to the list the command's constructor passes in.
type verbs []string

func (v verbs) Name() string                  { return v[0] }
func (v verbs) Aliases() []string             { return v }
func (v verbs) MatchCommand(line string) bool { _, ok := MatchAlias(line, v); return ok }

// argument is whatever follows the matched alias in line, trimmed ("" if
// no alias matches).
func (v verbs) argument(line string) string {
	alias, ok := MatchAlias(line, v)
	if !ok {
		return ""
	}
	return CommandArgument(line, alias)
}

// alwaysInRoom provides the MatchRoom stub every command shares for now.
type alwaysInRoom struct{}

func (alwaysInRoom) MatchRoom(*world.Room) bool { return true }

// quoted formats s the way the game echoes a player's words back:
// single-quoted, e.g. You can't go 'west' from here.
func quoted(s string) string {
	return "'" + s + "'"
}
