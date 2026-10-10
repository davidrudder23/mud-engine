package world

import (
	"errors"
	"slices"
	"testing"
)

func TestStats(t *testing.T) {
	if s := DefaultPlayerStats(); s.Dead() || s.Condition() != "unhurt" || s.HitPoints != s.MaxHitPoints ||
		s.Stamina != s.MaxStamina {
		t.Fatalf("a new player should start alive and full: %+v", s)
	}

	// Its own numbers, so retuning DefaultPlayerStats doesn't matter.
	s := Stats{HitPoints: 20, MaxHitPoints: 20, Stamina: 10, MaxStamina: 10}

	tests := []struct {
		hp        int
		condition string
		dead      bool
	}{
		{20, "unhurt", false},
		{11, "wounded", false}, // more than half
		{10, "badly wounded", false},
		{1, "badly wounded", false},
		{0, "badly wounded", true},  // dead at zero...
		{-3, "badly wounded", true}, // ...and below
	}
	for _, tt := range tests {
		s.HitPoints = tt.hp
		if s.Condition() != tt.condition || s.Dead() != tt.dead {
			t.Errorf("hp %d: condition=%q dead=%v", tt.hp, s.Condition(), s.Dead())
		}
	}

	s.Stamina = 0
	s.Restore()
	if s.HitPoints != s.MaxHitPoints || s.Stamina != s.MaxStamina {
		t.Errorf("after Restore: %+v", s)
	}
}

func TestBestWeapon(t *testing.T) {
	rag := &Item{ID: "rag"}
	knife := &Item{ID: "knife", Weapon: &Weapon{Damage: 2}}
	club := &Item{ID: "club", Weapon: &Weapon{Damage: 6}}

	if got := BestWeapon([]*Item{rag}); got != nil {
		t.Errorf("no weapons: got %v", got)
	}
	if got := BestWeapon([]*Item{knife, rag, club}); got != club {
		t.Errorf("got %v, want the club", got)
	}
	if (&Player{Inventory: []*Item{knife}}).Wielded() != knife {
		t.Error("player doesn't wield its weapon")
	}
}

func enemy() *NPC {
	return &NPC{
		ID: "troll", Name: "A Troll", Enemy: true,
		Stats:     &Stats{Strength: 8, HitPoints: 30, MaxHitPoints: 30, Stamina: 10, MaxStamina: 10},
		Inventory: []*Item{{ID: "club", Name: "a spiked club", Weapon: &Weapon{Damage: 6}}},
	}
}

func TestEnemyFormatShowsCondition(t *testing.T) {
	troll := enemy()
	if troll.Format() != "A Troll" {
		t.Errorf("unhurt: %q", troll.Format())
	}
	troll.Stats.HitPoints = 20
	if troll.Format() != "A Troll (wounded)" {
		t.Errorf("wounded: %q", troll.Format())
	}
	troll.Stats.HitPoints = 5
	if troll.Format() != "A Troll (badly wounded)" {
		t.Errorf("badly wounded: %q", troll.Format())
	}
}

func TestEnemyOffersAttack(t *testing.T) {
	if got := enemy().GetCommands(); !slices.Equal(got, []string{"talk", "attack"}) {
		t.Errorf("enemy: %v", got)
	}
	if got := (&NPC{ID: "grubb"}).GetCommands(); !slices.Equal(got, []string{"talk"}) {
		t.Errorf("helper: %v", got)
	}
}

func TestSpawnGivesEachInstanceItsOwnStatsAndWeapons(t *testing.T) {
	definition := enemy()
	a, b := definition.Spawn("cave"), definition.Spawn("cave")

	a.Stats.HitPoints = 1
	a.Inventory[0].Weapon.Damage = 99
	if b.Stats.HitPoints != 30 || definition.Stats.HitPoints != 30 {
		t.Error("hit points shared between spawns")
	}
	if b.Inventory[0].Weapon.Damage != 6 || definition.Inventory[0].Weapon.Damage != 6 {
		t.Error("weapon shared between spawns")
	}
	if a.CombatStats() != a.Stats || a.Wielded() != a.Inventory[0] || a.CombatName() != "A Troll" {
		t.Error("NPC combatant methods")
	}
}

func TestKillNPCDropsItsItemsAndLeavesTheRoom(t *testing.T) {
	w := New()
	w.Structures["s"] = &Structure{ID: "s", Name: "S"}
	room := &Room{ID: "cave", StructureID: "s", Items: []*Item{{ID: "rock"}}}
	w.AddRoom(room)
	troll := enemy().Spawn("cave")
	room.NPCs = append(room.NPCs, troll)
	club := troll.Inventory[0]

	dropped, err := w.KillNPC(troll)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(dropped, []*Item{club}) || !slices.Contains(room.Items, club) || len(room.Items) != 2 {
		t.Errorf("dropped=%v room items=%v", dropped, room.Items)
	}
	if slices.Contains(room.NPCs, troll) || troll.RoomID != "" || len(troll.Inventory) != 0 {
		t.Errorf("troll still placed: %+v", troll)
	}
	if _, err := w.KillNPC(troll); !errors.Is(err, ErrNotFound) { // already gone
		t.Errorf("second kill: %v", err)
	}
}

func TestStartingRoomIsTheFirstLoaded(t *testing.T) {
	w := New()
	w.AddRoom(&Room{ID: "b"})
	w.AddRoom(&Room{ID: "a"})
	if w.StartingRoomID() != "b" {
		t.Errorf("got %q", w.StartingRoomID())
	}
}

func TestLoadEnemyStatsAndWeapons(t *testing.T) {
	d := newWorldDir(t)
	d.structure(t, "s.json", obj{"id": "s", "name": "S"})
	d.npc(t, "troll.json", obj{
		"id": "troll", "name": "A Troll", "enemy": true,
		"stats":     obj{"strength": 8, "hit_points": 30, "stamina": 10, "luck": 2, "dexterity": 3},
		"inventory": []obj{{"id": "club", "name": "a spiked club", "weapon": obj{"damage": 6}}},
	})
	d.room(t, "r.json", obj{
		"id": "r", "structure_id": "s", "npcs": []string{"troll"},
		"items": []obj{{"id": "dagger", "weapon": obj{"damage": 3}}, {"id": "rock"}},
	})

	w := d.mustLoad(t)

	troll := mustRoom(t, w, "r").NPCs[0]
	want := Stats{Strength: 8, Dexterity: 3, Luck: 2, HitPoints: 30, MaxHitPoints: 30, Stamina: 10, MaxStamina: 10}
	if !troll.Enemy || troll.Stats == nil || *troll.Stats != want {
		t.Errorf("troll = %+v, stats %+v", troll, troll.Stats)
	}
	if weapon := troll.Inventory[0].Weapon; weapon == nil || weapon.Damage != 6 {
		t.Errorf("club weapon = %+v", weapon)
	}
	items := mustRoom(t, w, "r").Items
	if items[0].Weapon == nil || items[0].Weapon.Damage != 3 || items[1].Weapon != nil {
		t.Errorf("room items = %+v, %+v", items[0], items[1])
	}
}

// The MCP server returns these views; stats and weapons must show up
// in them, and stay out for things that have none.
func TestViewsIncludeStatsAndWeapons(t *testing.T) {
	troll := enemy()
	view := troll.View()
	if !view.Enemy || view.Stats == nil || view.Stats.HitPoints != troll.Stats.HitPoints ||
		view.Inventory[0].Weapon == nil || view.Inventory[0].Weapon.Damage != troll.Inventory[0].Weapon.Damage {
		t.Errorf("enemy view = %+v", view)
	}

	helper := (&NPC{ID: "grubb", Inventory: []*Item{{ID: "rag"}}}).View()
	if helper.Enemy || helper.Stats != nil || helper.Inventory[0].Weapon != nil {
		t.Errorf("helper view = %+v", helper)
	}

	stats := DefaultPlayerStats()
	player := (&Player{Name: "alice", Stats: &stats}).View()
	if player.Stats == nil || *player.Stats != *stats.View() {
		t.Errorf("player view stats = %+v", player.Stats)
	}
}

func TestLoadEnemyErrors(t *testing.T) {
	tests := map[string]obj{
		"enemy without stats": {"id": "troll", "name": "A Troll", "enemy": true},
	}
	// Every one of the five stats is required.
	for _, missing := range []string{"strength", "hit_points", "stamina", "luck", "dexterity"} {
		stats := obj{"strength": 8, "hit_points": 30, "stamina": 10, "luck": 2, "dexterity": 3}
		delete(stats, missing)
		tests["enemy missing "+missing] = obj{"id": "troll", "name": "A Troll", "enemy": true, "stats": stats}
	}
	for name, npc := range tests {
		t.Run(name, func(t *testing.T) {
			d := newWorldDir(t)
			d.structure(t, "s.json", obj{"id": "s", "name": "S"})
			d.npc(t, "troll.json", npc)
			var loadErr *LoadError
			if _, err := Load(string(d)); !errors.As(err, &loadErr) {
				t.Errorf("got %v, want a *LoadError", err)
			}
		})
	}
}
