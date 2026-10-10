package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"mudengine/internal/combat"
	"mudengine/internal/combat/combattest"
	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

// Rolls that decide an attack whatever the stats: every hit chance is
// at least combat.MinHitChance (> 0) and at most combat.MaxHitChance
// (< 100), and crit chance is at most combat.MaxCritChance (< 99). A
// fight's rolls go: player to-hit, player crit, enemy to-hit, enemy
// crit. Unscripted rolls miss (see combattest.Rolls).
const (
	hitRoll  = 0
	missRoll = 99
	noCrit   = 99
)

// Expected numbers come from the combat rules and the fixture's own
// stats, so retuning the rules or the default player doesn't break
// these tests -- only breaking the attack command does.

func (f *fixture) playerDamage() int {
	return combat.Damage(f.player.Stats, f.player.Wielded())
}

func trollDamage(troll *world.NPC) int {
	return combat.Damage(troll.Stats, troll.Wielded())
}

// addTroll puts an enemy troll, wielding a spiked club, in roomID.
func (f *fixture) addTroll(roomID string) *world.NPC {
	troll := &world.NPC{
		ID: "troll", Name: "A Troll", Enemy: true, RoomID: roomID,
		Stats: &world.Stats{
			Strength: 8, Dexterity: 5, Luck: 0,
			HitPoints: 30, MaxHitPoints: 30, Stamina: 10, MaxStamina: 10,
		},
		Inventory: []*world.Item{{ID: "club", Name: "a spiked club", Weapon: &world.Weapon{Damage: 6}}},
	}
	room := f.room(roomID)
	room.NPCs = append(room.NPCs, troll)
	return troll
}

func (f *fixture) attackWith(rolls ...int) *AttackCommand {
	attack := NewAttackCommand(f.sessions, f.world, f.repo)
	attack.rng = combattest.NewRolls(rolls...)
	return attack
}

func TestAttackTargets(t *testing.T) {
	f := newFixture(t)
	f.addTroll(worldtest.StartID)
	f.addTom("")
	attack := f.attackWith(hitRoll)

	tests := []struct{ line, want string }{
		{"attack", "Attack whom?"},
		{"attack ghost", "You don't see 'ghost' here."},
		// Only enemies can be fought.
		{"attack tom", "Old Tom isn't looking for a fight."},
	}
	for _, tt := range tests {
		if out := f.run(t, attack, tt.line); out != tt.want {
			t.Errorf("%q: got %q, want %q", tt.line, out, tt.want)
		}
	}
}

// Being a helper is what protects an NPC, not lacking stats.
func TestHelpersWithStatsStillCantBeAttacked(t *testing.T) {
	f := newFixture(t)
	guard := f.addTroll(worldtest.StartID)
	guard.ID, guard.Name, guard.Enemy = "guard", "A Guard", false

	if out := f.run(t, f.attackWith(hitRoll), "attack guard"); out != "A Guard isn't looking for a fight." {
		t.Errorf("got %q", out)
	}
	if guard.Stats.HitPoints != guard.Stats.MaxHitPoints {
		t.Error("the guard was hurt")
	}
}

func TestPlayersCantBeAttacked(t *testing.T) {
	f := newFixture(t)
	bob, _ := f.repo.Create(context.Background(), "bob", worldtest.StartID)
	f.world.PlacePlayer(bob)

	if out := f.run(t, f.attackWith(hitRoll), "attack bob"); out != "You don't see 'bob' here." {
		t.Errorf("got %q", out)
	}
	if bob.Stats.HitPoints != bob.Stats.MaxHitPoints {
		t.Error("bob was hurt")
	}
}

func TestAttackExchange(t *testing.T) {
	f := newFixture(t)
	troll := f.addTroll(worldtest.StartID)
	before := *f.player.Stats
	attack := f.attackWith(hitRoll, noCrit, hitRoll, noCrit)

	out := f.run(t, attack, "attack troll")

	// Both sides land one hit, and pay for it in stamina.
	wantPlayer := before.HitPoints - trollDamage(troll)
	if troll.Stats.HitPoints != troll.Stats.MaxHitPoints-f.playerDamage() ||
		f.player.Stats.HitPoints != wantPlayer ||
		f.player.Stats.Stamina != before.Stamina-combat.StaminaCost ||
		troll.Stats.Stamina != troll.Stats.MaxStamina-combat.StaminaCost {
		t.Errorf("troll %+v, player %+v", troll.Stats, f.player.Stats)
	}
	want := strings.Join([]string{
		fmt.Sprintf("You hit a Troll for %d damage.", f.playerDamage()),
		fmt.Sprintf("A Troll hits you with a spiked club for %d damage.", trollDamage(troll)),
		fmt.Sprintf("A Troll looks %s. You have %d/%d hit points and %d/%d stamina.",
			troll.Stats.Condition(), wantPlayer, before.MaxHitPoints,
			before.Stamina-combat.StaminaCost, before.MaxStamina),
	}, "\r\n")
	if out != want {
		t.Errorf("got %q\nwant %q", out, want)
	}

	// The exchange is persisted.
	saved, _ := f.repo.GetByID(context.Background(), f.player.ID)
	if *saved.Stats != *f.player.Stats {
		t.Errorf("saved %+v, want %+v", saved.Stats, f.player.Stats)
	}
}

func TestAttackNarration(t *testing.T) {
	troll := &world.NPC{Name: "A Troll"}
	club := &world.Item{Name: "a spiked club"}
	tests := []struct {
		name   string
		result combat.Result
		player string // the player attacking the troll
		enemy  string // the troll attacking the player
	}{
		{"tired", combat.Result{Tired: true},
			"You're too tired to attack.", "A Troll is too winded to strike back."},
		{"miss", combat.Result{},
			"You swing at a Troll and miss.", "A Troll swings at you and misses."},
		{"hit, bare hands", combat.Result{Hit: true, Damage: 3},
			"You hit a Troll for 3 damage.", "A Troll hits you for 3 damage."},
		{"hit with a weapon", combat.Result{Weapon: club, Hit: true, Damage: 3},
			"You hit a Troll with a spiked club for 3 damage.", "A Troll hits you with a spiked club for 3 damage."},
		{"crit", combat.Result{Weapon: club, Hit: true, Crit: true, Damage: 6},
			"You land a critical hit on a Troll with a spiked club for 6 damage!",
			"A Troll lands a critical hit on you with a spiked club for 6 damage!"},
	}
	for _, tt := range tests {
		if got := describePlayerAttack(troll, tt.result); got != tt.player {
			t.Errorf("%s, player: %q, want %q", tt.name, got, tt.player)
		}
		if got := describeEnemyAttack(troll, tt.result); got != tt.enemy {
			t.Errorf("%s, enemy: %q, want %q", tt.name, got, tt.enemy)
		}
	}
}

func TestTiredPlayerStillTakesTheEnemysTurn(t *testing.T) {
	f := newFixture(t)
	troll := f.addTroll(worldtest.StartID)
	f.player.Stats.Stamina = combat.StaminaCost - 1
	before := f.player.Stats.HitPoints

	out := f.run(t, f.attackWith(hitRoll, noCrit), "attack troll") // only the troll swings

	if troll.Stats.HitPoints != troll.Stats.MaxHitPoints || f.player.Stats.HitPoints != before-trollDamage(troll) {
		t.Errorf("troll %+v, player %+v", troll.Stats, f.player.Stats)
	}
	if !strings.HasPrefix(out, "You're too tired to attack.\r\nA Troll hits you") {
		t.Errorf("got %q", out)
	}
}

func TestEnemyWeaponsCantBeTakenWhileItLives(t *testing.T) {
	f := newFixture(t)
	f.addTroll(worldtest.StartID)

	if out := f.run(t, NewPickUpCommand(f.sessions, f.world), "pick up club"); out != "You don't see 'club' here." {
		t.Errorf("got %q", out)
	}
	if len(f.player.Inventory) != 0 {
		t.Error("took the living troll's club")
	}
}

func TestKillingAnEnemyDropsItsWeaponForPickUp(t *testing.T) {
	f := newFixture(t)
	troll := f.addTroll(worldtest.StartID)
	troll.Stats.HitPoints = f.playerDamage() // one hit leaves it at 0: dead
	before := f.player.Stats.HitPoints

	out := f.run(t, f.attackWith(hitRoll, noCrit, hitRoll, noCrit), "attack troll")

	if !strings.Contains(out, "A Troll collapses, dead.\r\nIt drops a spiked club.") {
		t.Errorf("got %q", out)
	}
	// Dead enemies don't strike back.
	if f.player.Stats.HitPoints != before || strings.Contains(out, "hits you") {
		t.Errorf("the dead troll struck back: %q", out)
	}
	if slices.Contains(f.room(worldtest.StartID).NPCs, troll) || troll.RoomID != "" {
		t.Error("dead troll still in the room")
	}

	if out := f.run(t, NewPickUpCommand(f.sessions, f.world), "pick up club"); out != "You pick up a spiked club." {
		t.Errorf("pick up: %q", out)
	}
	if weapon := f.player.Wielded(); weapon == nil || weapon.ID != "club" {
		t.Error("player isn't wielding the looted club")
	}
	if out := f.run(t, f.attackWith(hitRoll), "attack troll"); out != "You don't see 'troll' here." {
		t.Errorf("attacking the dead: %q", out)
	}
}

func TestPlayerDeathReturnsThemHealedToTheStart(t *testing.T) {
	f := newFixture(t)
	f.world.MovePlayer(f.player, worldtest.NorthID)
	troll := f.addTroll(worldtest.NorthID)
	rock := &world.Item{ID: "rock", Name: "a rock"}
	f.player.Inventory = []*world.Item{rock}
	f.player.Stats.HitPoints = trollDamage(troll) // one troll hit leaves 0: dead
	f.player.Stats.Stamina = combat.StaminaCost

	out := f.run(t, f.attackWith(missRoll, hitRoll, noCrit), "attack troll")

	start := f.world.StartingRoomID()
	if !strings.Contains(out, "You collapse, dead.") || !strings.Contains(out, "["+start+"]") {
		t.Errorf("got %q", out)
	}
	if f.player.CurrentRoomID != start || !slices.Contains(f.room(start).Players, f.player) ||
		slices.Contains(f.room(worldtest.NorthID).Players, f.player) {
		t.Errorf("player in %q", f.player.CurrentRoomID)
	}
	stats := f.player.Stats
	if stats.HitPoints != stats.MaxHitPoints || stats.Stamina != stats.MaxStamina || !slices.Contains(f.player.Inventory, rock) {
		t.Errorf("after death: stats %+v, inventory %v", stats, f.player.Inventory)
	}
	saved, _ := f.repo.GetByID(context.Background(), f.player.ID)
	if saved.CurrentRoomID != start || *saved.Stats != *stats {
		t.Errorf("saved: room %q, stats %+v", saved.CurrentRoomID, saved.Stats)
	}
}

func TestStatsCommand(t *testing.T) {
	f := newFixture(t)
	stats := NewStatsCommand(f.sessions)
	f.player.Stats = &world.Stats{
		Strength: 4, Dexterity: 6, Luck: 3, HitPoints: 12, MaxHitPoints: 21, Stamina: 7, MaxStamina: 9,
	}

	out := f.run(t, stats, "stats")
	for _, want := range []string{"12/21", "7/9", "Strength 4", "Dexterity 6", "Luck 3", "bare hands"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}

	f.player.Inventory = []*world.Item{{Name: "a spiked club", Weapon: &world.Weapon{Damage: 6}}}
	if out := f.run(t, stats, "stats"); !strings.Contains(out, "a spiked club (6 damage)") {
		t.Errorf("wielding: %q", out)
	}
}

func TestNarrationHelpers(t *testing.T) {
	for name, want := range map[string]string{
		"A Troll": "a Troll", "An Ogre": "an Ogre", "The Librarian": "the Librarian", "Grubb": "Grubb",
	} {
		if got := asObject(name); got != want {
			t.Errorf("asObject(%q) = %q", name, got)
		}
	}
	a, b, c := &world.Item{Name: "a club"}, &world.Item{Name: "a key"}, &world.Item{Name: "a rag"}
	for _, tt := range []struct {
		items []*world.Item
		want  string
	}{
		{[]*world.Item{a}, "a club"},
		{[]*world.Item{a, b}, "a club and a key"},
		{[]*world.Item{a, b, c}, "a club, a key, and a rag"},
	} {
		if got := itemList(tt.items); got != tt.want {
			t.Errorf("itemList = %q, want %q", got, tt.want)
		}
	}
}
