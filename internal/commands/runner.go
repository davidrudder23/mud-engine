package commands

import (
	"strings"

	"mudengine/internal/llm"
	"mudengine/internal/repository"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

// UnknownCommand is the reply for a line no command matches.
func UnknownCommand(line string) string {
	return "Unknown command: " + quoted(strings.TrimSpace(line))
}

// matchLocally finds the command line invokes without asking an LLM,
// returning it and the line to run it with. Matching goes in passes,
// stopping at the first pass where any command matches:
//
//  0. exact: MatchCommand, a verb-prefix check against each alias;
//  1. fuzzy: an alias within 1 edit of the typed verb (FuzzyMatchAlias),
//     run with the typo corrected;
//  2. fuzzy: within 2 edits (MaxFuzzyDistance).
//
// Each alias only takes part in the fuzzy passes its length allows
// (AllowedDistance: exact only for 1-3 letters, 2 edits for 4 or more).
// Within a pass, the earliest command in the list wins.
func matchLocally(line string, commands []Command) (Command, string, bool) {
	for _, command := range commands {
		if command.MatchCommand(line) {
			return command, line, true
		}
	}
	for distance := 1; distance <= MaxFuzzyDistance; distance++ {
		for _, command := range commands {
			if corrected, ok := FuzzyMatchAlias(line, command.Aliases(), distance); ok {
				llmRunnerLogger.Debug("Fuzzy-matched command",
					"line", line, "command", command.Name(), "corrected", corrected, "distance", distance)
				return command, corrected, true
			}
		}
	}
	return nil, "", false
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
		NewInventoryCommand(sessions),
		NewStatsCommand(sessions),
		NewAttackCommand(sessions, w, players),
		NewOrderCommand(),
		NewTalkCommand(sessions, w, talkLLM),
	}
	// HelpCommand reads the list at run time, through a pointer to this
	// variable -- appending itself last is what makes `help` list
	// itself too.
	commands = append(commands, NewHelpCommand(sessions, w, &commands))
	return commands
}
