package commands

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/session"
	"mudengine/internal/world"
)

// LookCommand re-describes the player's current room.
//
// Matches "look" plus anything after it ("look around") -- the rest is
// ignored for now, since there's nothing yet to look *at* besides the
// whole room. Also appends a "you can try" hint built from
// Room.GetCommands -- a preview of what's actionable here, not a
// substitute for `help`'s full list.
type LookCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
	world    *world.World
}

func NewLookCommand(sessions *session.Registry, w *world.World) *LookCommand {
	return &LookCommand{verbs: verbs{"look"}, sessions: sessions, world: w}
}

func (c *LookCommand) Describe() string        { return "look        re-describes the room you're in" }
func (c *LookCommand) Parameters() []Parameter { return nil }

func (c *LookCommand) RunCommand(_ context.Context, _ string, sessionID uuid.UUID) (string, error) {
	room, err := currentRoom(c.sessions, c.world, sessionID)
	if err != nil {
		return "", err
	}
	return LookText(room), nil
}

// LookText is what `look` shows for room: its description plus the
// "you can try" hint.
func LookText(room *world.Room) string {
	return room.Format() + "\r\nYou can try: " + strings.Join(room.GetCommands(), ", ")
}

// currentRoom returns the room sessionID's player is standing in.
func currentRoom(sessions *session.Registry, w *world.World, sessionID uuid.UUID) (*world.Room, error) {
	s, err := sessions.Get(sessionID)
	if err != nil {
		return nil, err
	}
	return w.GetRoom(s.Player.CurrentRoomID)
}
