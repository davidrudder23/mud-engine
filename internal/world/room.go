package world

import "strings"

// Room is a location, with static content (description, items,
// doorways) and live occupancy (players, NPCs).
type Room struct {
	ID string
	// Every room belongs to exactly one structure. Kept in sync with
	// Structure.Rooms via World.PlaceRoom / World.MoveRoomToStructure --
	// don't set this directly once the room is placed.
	StructureID string
	Description string
	Items       []*Item
	Doorways    []*Doorway
	// NPCs placed in this room. Each entry is its own independent
	// NPC.Spawn instance, not the shared definition in World.NPCs.
	NPCs []*NPC
	// Who's currently here. Live, in-memory occupancy -- only ever
	// mutated via World.PlacePlayer / MovePlayer / RemovePlayer.
	Players []*Player
	// Extra verbs authored directly on the room in JSON, on top of
	// whatever GetCommands derives from the room's own content -- so a
	// one-off room can offer a verb without a matching item/player.
	Commands []string
	// Non-nil if things can be bought and sold here.
	Shop *Shop
}

// Format renders the room as telnet-friendly text (CRLF line endings).
func (r *Room) Format() string {
	lines := []string{"[" + r.ID + "]", r.Description}

	if len(r.Items) > 0 {
		lines = append(lines, "You see:")
		for _, item := range r.Items {
			lines = append(lines, "  - "+item.Format())
		}
	}
	if len(r.NPCs) > 0 {
		lines = append(lines, "NPCs here:")
		for _, npc := range r.NPCs {
			lines = append(lines, "  - "+npc.Format())
		}
	}
	if len(r.Players) > 0 {
		lines = append(lines, "Players here:")
		for _, player := range r.Players {
			lines = append(lines, "  - "+player.Format())
		}
	}
	if len(r.Doorways) > 0 {
		lines = append(lines, "Doorways:")
		for _, doorway := range r.Doorways {
			lines = append(lines, "  - "+doorway.Format())
		}
	}

	return strings.Join(lines, "\r\n")
}

// RoomView is the JSON-serializable snapshot of a Room, for the MCP
// server -- the structured counterpart to Format(), with GetCommands'
// "you can try" list folded in too.
type RoomView struct {
	ID          string        `json:"id"`
	StructureID string        `json:"structure_id"`
	Description string        `json:"description"`
	Items       []ItemView    `json:"items"`
	NPCs        []NPCView     `json:"npcs"`
	Players     []PlayerView  `json:"players"`
	Doorways    []DoorwayView `json:"doorways"`
	Commands    []string      `json:"commands"`
	Shop        *ShopView     `json:"shop,omitempty"`
}

func (r *Room) View() RoomView {
	view := RoomView{
		ID:          r.ID,
		StructureID: r.StructureID,
		Description: r.Description,
		Items:       itemViews(r.Items),
		NPCs:        make([]NPCView, 0, len(r.NPCs)),
		Players:     make([]PlayerView, 0, len(r.Players)),
		Doorways:    make([]DoorwayView, 0, len(r.Doorways)),
		Commands:    r.GetCommands(),
		Shop:        r.Shop.View(),
	}
	for _, npc := range r.NPCs {
		view.NPCs = append(view.NPCs, npc.View())
	}
	for _, player := range r.Players {
		view.Players = append(view.Players, player.View())
	}
	for _, doorway := range r.Doorways {
		view.Doorways = append(view.Doorways, doorway.View())
	}
	return view
}

// GetCommands returns the verbs available in this room right now, for
// `look`'s "you can try" hint: always "look"; "go" if there's a
// doorway; whatever the items, NPCs, and players present each
// contribute; "browse", "buy", and "sell" if there's a shop; plus
// whatever this room's JSON authored onto Commands. Deduplicated, order
// preserved.
func (r *Room) GetCommands() []string {
	commands := []string{"look"}
	if len(r.Doorways) > 0 {
		commands = append(commands, "go")
	}
	for _, item := range r.Items {
		commands = append(commands, item.GetCommands()...)
	}
	for _, npc := range r.NPCs {
		commands = append(commands, npc.GetCommands()...)
	}
	for _, player := range r.Players {
		commands = append(commands, player.GetCommands()...)
	}
	if r.Shop != nil {
		commands = append(commands, "browse", "buy", "sell")
	}
	commands = append(commands, r.Commands...)
	return dedupe(commands)
}
