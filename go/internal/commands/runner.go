package commands

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/llm"
	"mudengine/internal/logging"
	"mudengine/internal/repository"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

var runnerLogger = logging.For("mud.commands.runner")

// Runner dispatches one line of input to a command and returns the
// text to show the player. CommandRunner and LLMCommandRunner both
// implement it, so the servers can use either interchangeably.
type Runner interface {
	Run(ctx context.Context, line string, sessionID uuid.UUID) (string, error)
}

// UnknownCommand is the reply for a line no command matches.
func UnknownCommand(line string) string {
	return "Unknown command: " + quoted(strings.TrimSpace(line))
}

// CommandRunner matches a line against known commands and runs
// whichever matches first. Matching goes in passes, stopping at the
// first pass where any command matches:
//
//  0. exact: MatchCommand, a verb-prefix check against each alias;
//  1. fuzzy: an alias within 1 edit of the typed verb (FuzzyMatchAlias);
//  2. fuzzy: within 2 edits (MaxFuzzyDistance).
//
// Each alias only takes part in the fuzzy passes its length allows
// (AllowedDistance: exact only for 1-3 letters, 1 edit for 4, 2 for 5+).
// No match in any pass is "Unknown command". Within a pass, the
// earliest command in the list wins.
type CommandRunner struct {
	commands []Command
}

func NewCommandRunner(commands []Command) *CommandRunner {
	return &CommandRunner{commands: commands}
}

func (r *CommandRunner) Run(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	for _, command := range r.commands {
		if command.MatchCommand(line) {
			return command.RunCommand(ctx, line, sessionID)
		}
	}
	for distance := 1; distance <= MaxFuzzyDistance; distance++ {
		for _, command := range r.commands {
			if corrected, ok := FuzzyMatchAlias(line, command.Aliases(), distance); ok {
				runnerLogger.Debug("Fuzzy-matched command",
					"line", line, "command", command.Name(), "corrected", corrected, "distance", distance)
				return command.RunCommand(ctx, corrected, sessionID)
			}
		}
	}
	runnerLogger.Debug("No command matched", "line", line)
	return UnknownCommand(line), nil
}

// BuildAll builds every command a session gets -- the one list both the
// telnet server and the MCP server use. talkLLM is how `talk` gets its
// LLM client (nil for llm.GetClient).
func BuildAll(sessions *session.Registry, w *world.World, players repository.PlayerRepository, talkLLM func() (llm.Client, error)) []Command {
	commands := []Command{
		NewLookCommand(sessions, w),
		NewGoCommand(sessions, w, players),
		NewQuitCommand(sessions),
		NewPickUpCommand(sessions, w),
		NewDropCommand(sessions, w),
		NewOrderCommand(),
		NewTalkCommand(sessions, w, talkLLM),
	}
	// HelpCommand reads the list at run time, through a pointer to this
	// variable -- appending itself last is what makes `help` list
	// itself too.
	commands = append(commands, NewHelpCommand(sessions, w, &commands))
	return commands
}
