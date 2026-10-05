package commands

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"

	"mudengine/internal/llm"
	"mudengine/internal/logging"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

var talkLogger = logging.For("mud.commands.talk")

const greetingPrompt = "Greet the player who just approached you, briefly and in character. " +
	"One or two sentences. No stage directions, no quotation marks around " +
	"the whole reply."

// TalkCommand gets one in-character line from an NPC in the room.
//
// Matches NPCs the same way `pick up`/`drop` match items: a
// case-insensitive substring against the NPC's id or name. Accepts
// either "talk <npc>" or "talk to <npc>".
//
// Single-shot: there's no conversation state and no player message to
// respond to -- the model improvises one greeting in character from the
// NPC's name and description alone.
//
// The LLM client is built lazily, on first use, not at construction --
// so a server nobody types `talk` on never needs LLM config. A failed
// build (e.g. no API key) is retried on the next `talk` rather than
// cached, in case the config gets fixed mid-session; a successful
// client is built once and reused.
type TalkCommand struct {
	alwaysInRoom
	verbs
	sessions     *session.Registry
	world        *world.World
	getLLMClient func() (llm.Client, error)

	mu        sync.Mutex
	llmClient llm.Client
}

// NewTalkCommand builds a TalkCommand. getLLMClient defaults to
// llm.GetClient when nil.
func NewTalkCommand(sessions *session.Registry, w *world.World, getLLMClient func() (llm.Client, error)) *TalkCommand {
	if getLLMClient == nil {
		getLLMClient = llm.GetClient
	}
	return &TalkCommand{verbs: verbs{"talk"}, sessions: sessions, world: w, getLLMClient: getLLMClient}
}

func (c *TalkCommand) Describe() string {
	return "talk to <npc>   strikes up a conversation with an NPC in the room"
}
func (c *TalkCommand) Parameters() []Parameter {
	return []Parameter{{Name: "npc", Type: String, Required: true}}
}

func (c *TalkCommand) client() (llm.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.llmClient == nil {
		client, err := c.getLLMClient()
		if err != nil {
			return nil, err
		}
		c.llmClient = client
	}
	return c.llmClient, nil
}

func (c *TalkCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	room, err := currentRoom(c.sessions, c.world, sessionID)
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

	reply, err := c.greet(ctx, npc)
	if errors.Is(err, llm.ErrConfig) || errors.Is(err, llm.ErrRequest) {
		talkLogger.Warn("talk failed", "npc", npc.Name, "error", err)
		return npc.Name + " doesn't seem to hear you.", nil
	}
	if err != nil {
		return "", err
	}
	return npc.Name + " says: " + strings.TrimSpace(reply), nil
}

func (c *TalkCommand) greet(ctx context.Context, npc *world.NPC) (string, error) {
	client, err := c.client()
	if err != nil {
		return "", err
	}
	system := strings.TrimSpace(
		"You are " + npc.Name + ", an NPC in a text-based multiplayer dungeon game. " + npc.Description,
	)
	return client.Complete(ctx, greetingPrompt, system)
}
