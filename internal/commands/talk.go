package commands

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/llm"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

// TalkCommand starts a conversation with an NPC in the room: the NPC,
// played by an LLM, greets the player, and from then on answers what
// they `say` (see npcChat) until one of them leaves the room or the
// player talks to someone else.
//
// Matches NPCs the same way `pick up`/`drop` match items: a
// case-insensitive substring against the NPC's id or name. Accepts
// either "talk <npc>" or "talk to <npc>".
type TalkCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
	world    *world.World
	chat     *npcChat
}

// NewTalkCommand builds a TalkCommand. getLLMClient defaults to
// llm.GetClient when nil.
func NewTalkCommand(sessions *session.Registry, w *world.World, getLLMClient func() (llm.Client, error)) *TalkCommand {
	return &TalkCommand{verbs: verbs{"talk"}, sessions: sessions, world: w, chat: newNPCChat(sessions, w, getLLMClient)}
}

func (c *TalkCommand) Describe() string {
	return "talk to <npc>   starts a conversation with an NPC in the room; then use say to answer"
}
func (c *TalkCommand) Parameters() []Parameter {
	return []Parameter{{Name: "npc", Type: String, Required: true}}
}

func (c *TalkCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	room, err := c.world.GetRoom(s.Player.CurrentRoomID)
	if err != nil {
		return "", err
	}

	query := c.argument(line)
	if strings.HasPrefix(strings.ToLower(query), "to ") {
		query = query[3:]
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return "Talk to whom?", nil
	}

	npc := findNPC(room.NPCs, query)
	if npc == nil {
		return "You don't see " + quoted(query) + " here.", nil
	}
	return c.chat.start(ctx, s, room, npc)
}
