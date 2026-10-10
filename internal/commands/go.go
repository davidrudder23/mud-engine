package commands

import (
	"context"

	"github.com/google/uuid"

	"mudengine/internal/repository"
	"mudengine/internal/scripting"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

// GoCommand moves through a doorway.
//
// Also answers to "leave", "exit", and "travel" ("leave north").
//
// Takes one parameter, the direction to go -- e.g. "go north". Matched
// against the current room's doorways by exact, case-sensitive
// comparison (so "go North" won't match a doorway whose direction is
// "north"). Moves the player via World.MovePlayer, then persists the
// move so it survives a restart/reconnect. Scripted NPCs in the new
// room then get their on_enter.
type GoCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
	world    *world.World
	players  repository.PlayerRepository
	// Nil for no scripting (see BuildAll).
	scripts *scripting.Engine
}

func NewGoCommand(sessions *session.Registry, w *world.World, players repository.PlayerRepository) *GoCommand {
	return &GoCommand{verbs: verbs{"go", "leave", "exit", "travel"}, sessions: sessions, world: w, players: players}
}

func (c *GoCommand) Describe() string {
	return `go <dir>    moves through a doorway (e.g. "go north")`
}
func (c *GoCommand) Parameters() []Parameter {
	return []Parameter{{Name: "direction", Type: String, Required: true}}
}

func (c *GoCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	player := s.Player

	direction := c.argument(line)
	if direction == "" {
		return "Go where?", nil
	}

	current, err := c.world.GetRoom(player.CurrentRoomID)
	if err != nil {
		return "", err
	}
	var doorway *world.Doorway
	for _, d := range current.Doorways {
		if d.Direction == direction {
			doorway = d
			break
		}
	}
	if doorway == nil {
		return "You can't go " + quoted(direction) + " from here.", nil
	}

	if err := c.world.MovePlayer(player, doorway.To); err != nil {
		return "", err
	}
	if err := c.players.Save(ctx, player); err != nil {
		return "", err
	}
	next, err := c.world.GetRoom(player.CurrentRoomID)
	if err != nil {
		return "", err
	}
	// The room as the player walked into it, then whatever its NPCs
	// did about that.
	return withLines(next.Format(), c.scripts.PlayerEntered(ctx, player)), nil
}
