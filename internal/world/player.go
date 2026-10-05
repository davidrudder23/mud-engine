package world

import (
	"time"

	"github.com/google/uuid"
)

// Player is a character in the world.
//
// Unlike rooms/items (static, authored content loaded from JSON),
// players are dynamic, multi-user state -- created and changed
// constantly as people play -- so they're persisted in the database
// instead (see the repository package).
//
// A Player is a distinct *instance*, not just a name: logging in twice
// under the same name (from two different Sessions) deliberately gets
// two separate *Player values that both refer to the same underlying
// row -- each moves independently and both can be in a room's occupant
// list at once. World compares players by pointer for exactly this
// reason.
type Player struct {
	// uuid.Nil until the player has been persisted.
	ID            uuid.UUID
	Name          string
	CurrentRoomID string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// What this player is carrying. Like Room.Players, this is live,
	// in-memory state, not persisted to the players table -- picking
	// something up only lasts for the session.
	Inventory []*Item
	// What this player fights with. Persisted alongside the room; the
	// repository fills it in (DefaultPlayerStats for a new player).
	Stats *Stats
}

func (p *Player) Format() string {
	return p.Name
}

// CombatName, CombatStats, and Wielded make a Player a combat.Combatant.
func (p *Player) CombatName() string  { return p.Name }
func (p *Player) CombatStats() *Stats { return p.Stats }
func (p *Player) Wielded() *Item      { return BestWeapon(p.Inventory) }

// PlayerView is the JSON-serializable snapshot of a Player. ID is nil
// (JSON null) for a player that was never persisted.
type PlayerView struct {
	ID        *string    `json:"id"`
	Name      string     `json:"name"`
	Inventory []ItemView `json:"inventory"`
	Stats     *StatsView `json:"stats,omitempty"`
}

func (p *Player) View() PlayerView {
	var id *string
	if p.ID != uuid.Nil {
		s := p.ID.String()
		id = &s
	}
	return PlayerView{ID: id, Name: p.Name, Inventory: itemViews(p.Inventory), Stats: p.Stats.View()}
}

// GetCommands returns the verbs this player's presence makes available,
// for `look`'s "you can try" hint. Nothing yet, so this is always empty.
func (p *Player) GetCommands() []string {
	return []string{}
}
