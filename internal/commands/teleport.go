package commands

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/repository"
	"mudengine/internal/scripting"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

// TeleportCommand moves the player straight to any room in the world,
// doorways or not.
//
// The room is named by its id ("teleport uu-library"), matched
// case-insensitively: an exact id wins; failing that, an id containing
// what was typed, as long as only one does ("teleport library"). When
// several do, they're listed instead and nobody moves.
//
// Otherwise it's `go`: the move goes through World.MovePlayer, is
// persisted, and scripted NPCs in the new room get their on_enter.
type TeleportCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
	world    *world.World
	players  repository.PlayerRepository
	// Nil for no scripting (see BuildAll).
	scripts *scripting.Engine
}

func NewTeleportCommand(sessions *session.Registry, w *world.World, players repository.PlayerRepository) *TeleportCommand {
	return &TeleportCommand{verbs: verbs{"teleport"}, sessions: sessions, world: w, players: players}
}

func (c *TeleportCommand) Describe() string {
	return "teleport <room>  moves you straight to a room, by its id"
}
func (c *TeleportCommand) Parameters() []Parameter {
	return []Parameter{{Name: "room", Type: String, Required: true}}
}

// findRooms returns the rooms query (already lowercased) names: the one
// whose id is exactly query, or else every room whose id contains it.
func findRooms(rooms []*world.Room, query string) []*world.Room {
	var matches []*world.Room
	for _, room := range rooms {
		id := strings.ToLower(room.ID)
		if id == query {
			return []*world.Room{room}
		}
		if strings.Contains(id, query) {
			matches = append(matches, room)
		}
	}
	return matches
}

func (c *TeleportCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	player := s.Player

	query := strings.ToLower(c.argument(line))
	if query == "" {
		return "Teleport where?", nil
	}
	matches := findRooms(c.world.Rooms(), query)
	switch {
	case len(matches) == 0:
		return "There's no room called " + quoted(query) + ".", nil
	case len(matches) > 1:
		ids := make([]string, 0, len(matches))
		for _, room := range matches {
			ids = append(ids, room.ID)
		}
		return quoted(query) + " could be any of: " + strings.Join(ids, ", ") + ".", nil
	}
	destination := matches[0]
	if destination.ID == player.CurrentRoomID {
		return "You're already there.", nil
	}

	if err := c.world.MovePlayer(player, destination.ID); err != nil {
		return "", err
	}
	if err := c.players.Save(ctx, player); err != nil {
		return "", err
	}
	return withLines(destination.Format(), c.scripts.PlayerEntered(ctx, player)), nil
}
