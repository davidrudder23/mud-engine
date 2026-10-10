// Package world holds the game's content and live state: structures,
// rooms, items, doorways, NPCs, players, and the World that ties them
// together (placement/movement bookkeeping), plus the JSON loader.
//
// Identity matters throughout: two Players (or NPCs) with identical
// field values are still distinct -- two logins under the same name are
// two separate Player instances, and two spawns of the same NPC
// definition are two separate NPCs. Everything here is therefore held
// and compared by pointer, never by value.
package world

// Item is an object that lives in a room (or in someone's inventory).
type Item struct {
	ID          string
	Name        string
	Description string
	// Non-nil if this item can be fought with.
	Weapon *Weapon
	// The Lua file (in World.ScriptsDir) giving this item behaviour
	// beyond the defaults; empty for none. See the scripting package.
	Script string
}

// Weapon is what makes an Item usable in combat.
type Weapon struct {
	// Added to the wielder's strength bonus on every hit.
	Damage int
}

func (i *Item) Format() string {
	return i.Name
}

// clone returns an independent copy of the item, weapon included.
func (i *Item) clone() *Item {
	copied := *i
	if i.Weapon != nil {
		weapon := *i.Weapon
		copied.Weapon = &weapon
	}
	return &copied
}

// ItemView is the JSON-serializable snapshot of an Item, for the MCP
// server -- the structured counterpart to Format().
type ItemView struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Weapon      *WeaponView `json:"weapon,omitempty"`
}

// WeaponView is the JSON-serializable snapshot of a Weapon.
type WeaponView struct {
	Damage int `json:"damage"`
}

func (i *Item) View() ItemView {
	view := ItemView{ID: i.ID, Name: i.Name, Description: i.Description}
	if i.Weapon != nil {
		view.Weapon = &WeaponView{Damage: i.Weapon.Damage}
	}
	return view
}

// BestWeapon returns the highest-damage weapon among items, or nil if
// there is none. Whoever carries it fights with it.
func BestWeapon(items []*Item) *Item {
	var best *Item
	for _, item := range items {
		if item.Weapon != nil && (best == nil || item.Weapon.Damage > best.Weapon.Damage) {
			best = item
		}
	}
	return best
}

// GetCommands returns the verbs this item makes available, for `look`'s
// "you can try" hint. Every item can be picked up, so this is constant
// for now -- it becomes real once items can restrict which of these
// apply (e.g. something bolted down).
func (i *Item) GetCommands() []string {
	return []string{"pick up"}
}

func itemViews(items []*Item) []ItemView {
	views := make([]ItemView, 0, len(items))
	for _, item := range items {
		views = append(views, item.View())
	}
	return views
}

// dedupe returns values with duplicates removed, first occurrence
// order preserved.
func dedupe(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// removePtr removes the first element of s that is the same pointer as
// target, reporting whether it found one.
func removePtr[T any](s []*T, target *T) ([]*T, bool) {
	for i, v := range s {
		if v == target {
			return append(s[:i], s[i+1:]...), true
		}
	}
	return s, false
}

func containsPtr[T any](s []*T, target *T) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}

// RemoveItem removes item (by identity) from items, reporting whether
// it was there. For commands moving items between rooms and
// inventories.
func RemoveItem(items []*Item, item *Item) ([]*Item, bool) {
	return removePtr(items, item)
}
