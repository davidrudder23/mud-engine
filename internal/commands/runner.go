package commands

import (
	"strings"

	"mudengine/internal/config"
	"mudengine/internal/llm"
	"mudengine/internal/repository"
	"mudengine/internal/scripting"
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

// withLines appends lines (what a script hook had to say to this
// player) to a command's reply.
func withLines(reply string, lines []string) string {
	if len(lines) == 0 {
		return reply
	}
	return reply + "\r\n" + strings.Join(lines, "\r\n")
}

// BuildAll builds every command a session gets -- the one list both the
// telnet server and the MCP server use. talkLLM is how NPC
// conversations (`talk`, and `say` once one's started) get their LLM
// client (nil for llm.GetClient). scripts is the Lua engine whose
// hooks the commands call; nil for a world with no scripting, which
// also leaves out the `reload` and `unload` commands.
func BuildAll(sessions *session.Registry, w *world.World, players repository.PlayerRepository, talkLLM func() (llm.Client, error), scripts *scripting.Engine) []Command {
	goCommand := NewGoCommand(sessions, w, players)
	pickUp := NewPickUpCommand(sessions, w)
	drop := NewDropCommand(sessions, w)
	attack := NewAttackCommand(sessions, w, players)
	say := NewSayCommand(sessions)
	teleport := NewTeleportCommand(sessions, w, players)
	goCommand.scripts, pickUp.scripts, drop.scripts, attack.scripts, say.scripts = scripts, scripts, scripts, scripts, scripts
	throw := NewThrowCommand(sessions, w, players)
	teleport.scripts, throw.scripts = scripts, scripts
	talk := NewTalkCommand(sessions, w, talkLLM)
	say.chat = talk.chat

	commands := []Command{
		NewLookCommand(sessions, w),
		goCommand,
		NewQuitCommand(sessions),
		pickUp,
		drop,
		NewInventoryCommand(sessions),
		NewStatsCommand(sessions),
		attack,
		throw,
		NewBrowseCommand(sessions, w),
		NewBuyCommand(sessions, w, players),
		NewSellCommand(sessions, w, players),
		NewOrderCommand(),
		talk,
		say,
		teleport,
	}
	if scripts != nil {
		commands = append(commands,
			NewReloadCommand(sessions, scripts, config.IsWizzard),
			NewUnloadCommand(sessions, scripts, config.IsWizzard),
		)
	}
	// HelpCommand reads the list at run time, through a pointer to this
	// variable -- appending itself last is what makes `help` list
	// itself too.
	commands = append(commands, NewHelpCommand(sessions, w, &commands))
	return commands
}
