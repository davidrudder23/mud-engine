package commands

import (
	"context"

	"github.com/google/uuid"

	"mudengine/internal/scripting"
	"mudengine/internal/session"
)

// SayCommand speaks to the room: every other session whose player
// stands in the speaker's room is sent "<name> says: <text>", and the
// speaker gets "You say: <text>" back. The text is passed along as
// typed, case and all.
//
// "Other" is per session, not per name: someone logged in twice hears
// their own other connection. NPCs don't hear any of it, except
// scripted ones, which get their on_say, and whichever one the speaker
// is in a conversation with (see TalkCommand), which answers.
type SayCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
	// Nil for no scripting (see BuildAll).
	scripts *scripting.Engine
	// Nil for no NPC conversations (see BuildAll).
	chat *npcChat
}

func NewSayCommand(sessions *session.Registry) *SayCommand {
	return &SayCommand{verbs: verbs{"say"}, sessions: sessions}
}

func (c *SayCommand) Describe() string {
	return "say <text>      says something to everyone in the room, including whoever you're talking to"
}
func (c *SayCommand) Parameters() []Parameter {
	return []Parameter{{Name: "text", Type: String, Required: true}}
}

func (c *SayCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	text := c.argument(line)
	if text == "" {
		return "Say what?", nil
	}

	message := s.Player.Name + " says: " + text
	for _, listener := range c.sessions.InRoom(s.Player.CurrentRoomID) {
		if listener.ID != sessionID {
			listener.Send(message)
		}
	}
	reply := "You say: " + text
	if c.chat != nil {
		answer, err := c.chat.hear(ctx, s, text)
		if err != nil {
			return "", err
		}
		if answer != "" {
			reply += "\r\n" + answer
		}
	}
	return withLines(reply, c.scripts.PlayerSaid(ctx, s.Player, text)), nil
}
