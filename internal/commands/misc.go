package commands

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/session"
	"mudengine/internal/world"
)

// QuitCommand closes the connection. Commands only return text -- they
// don't have the connection -- so this works by setting a flag on the
// session; the telnet server's read loop checks it after each command
// and closes the connection if it's set.
type QuitCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
}

func NewQuitCommand(sessions *session.Registry) *QuitCommand {
	return &QuitCommand{verbs: verbs{"quit"}, sessions: sessions}
}

func (c *QuitCommand) Describe() string        { return "quit        closes the connection" }
func (c *QuitCommand) Parameters() []Parameter { return nil }

func (c *QuitCommand) RunCommand(_ context.Context, _ string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	s.ShouldClose = true
	return "", nil
}

// OrderCommand orders something from whoever's serving.
//
// Pure flavor for now -- no drink inventory, no gold, no bartender to
// check with. It just echoes back what was ordered. Takes no
// room/session state, since there's nothing yet to act on.
type OrderCommand struct {
	alwaysInRoom
	verbs
}

func NewOrderCommand() *OrderCommand {
	return &OrderCommand{verbs: verbs{"order"}}
}

func (c *OrderCommand) Describe() string {
	return "order <drink>   orders a drink from whoever's serving"
}
func (c *OrderCommand) Parameters() []Parameter {
	return []Parameter{{Name: "drink", Type: String, Required: true}}
}

func (c *OrderCommand) RunCommand(_ context.Context, line string, _ uuid.UUID) (string, error) {
	drink := c.argument(line)
	if drink == "" {
		return "Order what?", nil
	}
	return "You order " + drink + ". It's set down in front of you without much ceremony.", nil
}

// HelpCommand lists every command usable in the player's current room.
//
// Two sources feed the listing:
//
//  1. The full command list (including itself, if it's in there),
//     filtered by MatchRoom for the player's current room, shown via
//     their Describe text.
//  2. Room.GetCommands -- the room's doorways, its items' / NPCs' /
//     players' own GetCommands, and its JSON-authored Commands.
//     Anything there not already covered by a registered command (e.g.
//     "pick up" from an item, or a JSON-configured "order drink") is
//     appended too, so `help` doesn't miss verbs that don't have a
//     real Command implementation yet.
type HelpCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
	world    *world.World
	// Read at run time, so a caller can append the HelpCommand itself
	// after constructing it (see BuildAll) -- as long as it passes a
	// pointer to the same slice.
	commands *[]Command
}

func NewHelpCommand(sessions *session.Registry, w *world.World, commands *[]Command) *HelpCommand {
	return &HelpCommand{verbs: verbs{"help"}, sessions: sessions, world: w, commands: commands}
}

func (c *HelpCommand) Describe() string        { return "help        lists the commands you can use" }
func (c *HelpCommand) Parameters() []Parameter { return nil }

func (c *HelpCommand) RunCommand(_ context.Context, _ string, sessionID uuid.UUID) (string, error) {
	room, err := currentRoom(c.sessions, c.world, sessionID)
	if err != nil {
		return "", err
	}

	var matching []Command
	var lines []string
	for _, command := range *c.commands {
		if command.MatchRoom(room) {
			matching = append(matching, command)
			lines = append(lines, command.Describe())
		}
	}

	// Room-derived verbs no registered command already covers.
	for _, verb := range room.GetCommands() {
		covered := false
		for _, command := range matching {
			if command.MatchCommand(verb) {
				covered = true
				break
			}
		}
		if !covered {
			lines = append(lines, verb)
		}
	}

	return strings.Join(lines, "\r\n"), nil
}
