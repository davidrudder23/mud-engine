package commands

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/session"
)

// InventoryCommand lists what the player is carrying -- what `pick up`
// added and `drop` hasn't taken away yet. Ignores anything typed after
// the verb.
type InventoryCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
}

func NewInventoryCommand(sessions *session.Registry) *InventoryCommand {
	return &InventoryCommand{verbs: verbs{"inventory"}, sessions: sessions}
}

func (c *InventoryCommand) Describe() string        { return "inventory   lists what you're carrying" }
func (c *InventoryCommand) Parameters() []Parameter { return nil }

func (c *InventoryCommand) RunCommand(_ context.Context, _ string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	if len(s.Player.Inventory) == 0 {
		return "You aren't carrying anything.", nil
	}

	lines := []string{"You are carrying:"}
	for _, item := range s.Player.Inventory {
		lines = append(lines, "  - "+item.Format())
	}
	return strings.Join(lines, "\r\n"), nil
}
