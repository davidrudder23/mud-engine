package world

import "time"

// DefaultMoveInterval is how often a wandering NPC moves when its
// definition doesn't say.
const DefaultMoveInterval = 120 * time.Second

// NPC is a non-player character. Starting with "helpers"; enemies come
// later.
//
// An NPC value plays two roles, the same type either way:
//
//  1. A definition -- static, authored content like Item/Room/Structure,
//     loaded once per id from world/npcs/*.json and held in World.NPCs.
//     This is the template: what a "tavern-keeper" or "shopkeeper" is.
//  2. A spawned instance -- placed into a specific room's Room.NPCs via
//     Spawn, with its own independent, live state (right now just
//     Inventory, since that's the only thing anything mutates).
//
// A definition is really just an NPC nobody has spawned yet (RoomID
// empty). One definition can back many simultaneous instances: the same
// "shopkeeper" placed in five rooms, or twice in one room, is five (or
// two) fully independent NPCs. Trading away one's stock doesn't touch
// another's, or the definition's own Inventory (which nothing should
// mutate directly -- it exists only as the template each spawn copies).
//
// Like Player, NPCs are compared by pointer: two fresh spawns of the
// same definition look identical field-by-field, but must stay distinct.
type NPC struct {
	ID          string
	Name        string
	Description string
	// Enemies can be attacked and fight back (see the combat package);
	// every other NPC is a helper and can't be attacked.
	Enemy bool
	// What an enemy fights with. Every enemy has them (the loader
	// requires it); nil for a helper.
	Stats *Stats
	// Verbs this NPC makes available beyond the baseline in
	// GetCommands (e.g. "trade") -- mirrors Room.Commands. Nothing
	// consumes an entry here unless a matching Command exists (today,
	// only "talk" does).
	Commands []string
	// What this NPC is carrying. Live, in-memory state, same as
	// Player.Inventory and Room.Players; not persisted.
	Inventory []*Item
	// Which room this instance stands in -- empty on a definition,
	// which isn't standing anywhere; set by Spawn.
	RoomID string
	// Non-nil for an NPC that wanders on its own (a JSON
	// `"type": "wandering"` definition). The moving itself doesn't live
	// here -- an NPC is still just data -- see the npcpatrol package.
	Wander *Wander
	// The Lua file (in World.ScriptsDir) giving this NPC behaviour
	// beyond the defaults; empty for none. See the scripting package.
	Script string
}

// Wander is the extra configuration a wandering NPC carries: it moves
// through a random doorway of whatever room it's in every MoveInterval.
type Wander struct {
	MoveInterval time.Duration
}

// Format is the NPC's name, plus its condition once an enemy has been
// hurt ("A Troll (wounded)").
func (n *NPC) Format() string {
	if n.Enemy && n.Stats != nil && n.Stats.HitPoints < n.Stats.MaxHitPoints {
		return n.Name + " (" + n.Stats.Condition() + ")"
	}
	return n.Name
}

// CombatName, CombatStats, and Wielded make an NPC a combat.Combatant.
func (n *NPC) CombatName() string  { return n.Name }
func (n *NPC) CombatStats() *Stats { return n.Stats }
func (n *NPC) Wielded() *Item      { return BestWeapon(n.Inventory) }

// IsWandering reports whether this NPC moves around on its own.
func (n *NPC) IsWandering() bool {
	return n.Wander != nil
}

// Spawn creates a new, independent NPC instance from this definition
// (or from any existing instance -- spawning a spawn works fine, it
// just copies whatever's currently on this one) and sets its RoomID to
// roomID: same id/name/description/enemy/commands/wander config, but
// its own copies of Commands, Inventory, and Stats (so each spawn has
// its own hit points), and its own identity.
//
// This only builds the instance -- the caller still adds it to that
// room's Room.NPCs (the loader does, right after calling this), the
// same way World.PlacePlayer is a separate step from constructing a
// Player.
func (n *NPC) Spawn(roomID string) *NPC {
	spawned := *n
	spawned.Commands = append([]string(nil), n.Commands...)
	spawned.Inventory = make([]*Item, 0, len(n.Inventory))
	for _, item := range n.Inventory {
		spawned.Inventory = append(spawned.Inventory, item.clone())
	}
	spawned.Stats = n.Stats.clone()
	if n.Wander != nil {
		wander := *n.Wander
		spawned.Wander = &wander
	}
	spawned.RoomID = roomID
	return &spawned
}

// NPCView is the JSON-serializable snapshot of an NPC. MoveInterval
// (seconds) is only present for a wandering NPC.
type NPCView struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Enemy        bool       `json:"enemy"`
	Commands     []string   `json:"commands"`
	Inventory    []ItemView `json:"inventory"`
	Stats        *StatsView `json:"stats,omitempty"`
	MoveInterval *float64   `json:"move_interval,omitempty"`
}

func (n *NPC) View() NPCView {
	view := NPCView{
		ID:          n.ID,
		Name:        n.Name,
		Description: n.Description,
		Enemy:       n.Enemy,
		Commands:    append([]string{}, n.Commands...),
		Inventory:   itemViews(n.Inventory),
		Stats:       n.Stats.View(),
	}
	if n.Wander != nil {
		seconds := n.Wander.MoveInterval.Seconds()
		view.MoveInterval = &seconds
	}
	return view
}

// GetCommands returns the verbs this NPC offers, for `look`'s "you can
// try" hint once it's placed in a room. Always includes "talk" -- every
// NPC can be talked to -- plus "attack" for an enemy, plus whatever's
// authored onto Commands, deduplicated and order-preserved. Nothing is
// derived from Inventory yet.
func (n *NPC) GetCommands() []string {
	commands := []string{"talk"}
	if n.Enemy {
		commands = append(commands, "attack")
	}
	return dedupe(append(commands, n.Commands...))
}
