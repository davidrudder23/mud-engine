package commands

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/scripting"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

// findItem returns the first item whose id or name contains query
// (already lowercased), case-insensitively.
func findItem(items []*world.Item, query string) *world.Item {
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.ID), query) || strings.Contains(strings.ToLower(item.Name), query) {
			return item
		}
	}
	return nil
}

// findNPC returns the first NPC whose id or name contains query
// (already lowercased), case-insensitively.
func findNPC(npcs []*world.NPC, query string) *world.NPC {
	for _, npc := range npcs {
		if strings.Contains(strings.ToLower(npc.ID), query) || strings.Contains(strings.ToLower(npc.Name), query) {
			return npc
		}
	}
	return nil
}

// PickUpCommand moves an item from the current room into the player's
// inventory.
//
// Matches items by a case-insensitive substring against either the
// item's id or its name, so "pick up tankard" finds an item whose id is
// "tankard" and "pick up half-finished" finds one named "a
// half-finished tankard". A scripted item then gets its on_pickup.
type PickUpCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
	world    *world.World
	// Nil for no scripting (see BuildAll).
	scripts *scripting.Engine
}

func NewPickUpCommand(sessions *session.Registry, w *world.World) *PickUpCommand {
	return &PickUpCommand{verbs: verbs{"pick up"}, sessions: sessions, world: w}
}

func (c *PickUpCommand) Describe() string { return "pick up <item>   picks up an item from the room" }
func (c *PickUpCommand) Parameters() []Parameter {
	return []Parameter{{Name: "item", Type: String, Required: true}}
}

func (c *PickUpCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	room, err := c.world.GetRoom(s.Player.CurrentRoomID)
	if err != nil {
		return "", err
	}

	query := strings.ToLower(c.argument(line))
	if query == "" {
		return "Pick up what?", nil
	}
	item := findItem(room.Items, query)
	if item == nil {
		return "You don't see " + quoted(query) + " here.", nil
	}

	room.Items, _ = world.RemoveItem(room.Items, item)
	s.Player.Inventory = append(s.Player.Inventory, item)
	return withLines("You pick up "+item.Format()+".", c.scripts.ItemPickedUp(ctx, item, s.Player)), nil
}

// DropCommand moves an item from the player's inventory back into the
// current room -- the reverse of `pick up`, matching items the same way
// but against the player's inventory instead of the room. A scripted
// item then gets its on_drop.
type DropCommand struct {
	alwaysInRoom
	verbs
	sessions *session.Registry
	world    *world.World
	// Nil for no scripting (see BuildAll).
	scripts *scripting.Engine
}

func NewDropCommand(sessions *session.Registry, w *world.World) *DropCommand {
	return &DropCommand{verbs: verbs{"drop"}, sessions: sessions, world: w}
}

func (c *DropCommand) Describe() string {
	return "drop <item>      drops an item from your inventory into the room"
}
func (c *DropCommand) Parameters() []Parameter {
	return []Parameter{{Name: "item", Type: String, Required: true}}
}

func (c *DropCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	s, err := c.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	room, err := c.world.GetRoom(s.Player.CurrentRoomID)
	if err != nil {
		return "", err
	}

	query := strings.ToLower(c.argument(line))
	if query == "" {
		return "Drop what?", nil
	}
	item := findItem(s.Player.Inventory, query)
	if item == nil {
		return "You aren't carrying " + quoted(query) + ".", nil
	}

	s.Player.Inventory, _ = world.RemoveItem(s.Player.Inventory, item)
	room.Items = append(room.Items, item)
	return withLines("You drop "+item.Format()+".", c.scripts.ItemDropped(ctx, item, s.Player)), nil
}
