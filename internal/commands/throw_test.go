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

const farID = "far-room"

// throwFixture is the usual fixture stretched to three rooms in a row
// -- start, north-room, far-room, each "north" of the last -- with
// alice at the south end carrying a spear and a rock.
type throwFixture struct {
	*fixture
	spear *world.Item
}

func newThrowFixture(t *testing.T) *throwFixture {
	t.Helper()
	f := newFixture(t)
	far := &world.Room{ID: farID, StructureID: worldtest.StructureID, Description: "The far end.",
		Doorways: []*world.Doorway{{Direction: "south", To: worldtest.NorthID}}}
	f.world.AddRoom(far)
	if err := f.world.PlaceRoom(far); err != nil {
		t.Fatal(err)
	}
	north := f.room(worldtest.NorthID)
	north.Doorways = append(north.Doorways, &world.Doorway{Direction: "north", To: farID})

	spear := &world.Item{ID: "spear", Name: "a spear", Weapon: &world.Weapon{Damage: 5, Type: world.WeaponSpear}}
	f.player.Inventory = []*world.Item{spear, {ID: "rock", Name: "a rock"}, {ID: "club", Name: "a club", Weapon: &world.Weapon{Damage: 9}}}
	return &throwFixture{fixture: f, spear: spear}
}

func (f *throwFixture) throwWith(rolls ...int) *ThrowCommand {
	throw := NewThrowCommand(f.sessions, f.world, f.repo)
	throw.rng = combattest.NewRolls(rolls...)
	return throw
}

// spearRoom is the id of the room the spear is lying in, or "" if it's
// still being carried.
func (f *throwFixture) spearRoom() string {
	for _, room := range f.world.Rooms() {
		if slices.Contains(room.Items, f.spear) {
			return room.ID
		}
	}
	return ""
}

func (f *throwFixture) spearDamage() int {
	return combat.Damage(f.player.Stats, f.spear)
}

func TestThrowNeedsASpearADirectionAndStamina(t *testing.T) {
	f := newThrowFixture(t)
	tests := []struct{ line, want string }{
		{"throw", "Throw what?"},
		{"throw spear", "Throw spear which way?"},
		{"throw sword north", "You aren't carrying 'sword'."},
		{"throw rock north", "You can't throw a rock."},
		// A weapon, but not a spear.
		{"throw club north", "You can't throw a club."},
	}
	for _, tt := range tests {
		if got := f.run(t, f.throwWith(), tt.line); got != tt.want {
			t.Errorf("%q = %q, want %q", tt.line, got, tt.want)
		}
	}
	f.player.Stats.Stamina = combat.StaminaCost - 1
	if got := f.run(t, f.throwWith(), "throw spear north"); got != "You're too tired to throw anything." {
		t.Errorf("too tired = %q", got)
	}
	if f.spearRoom() != "" || len(f.player.Inventory) != 3 {
		t.Error("a throw that didn't happen moved the spear")
	}
}

func TestThrowFliesUntilItFindsAnEnemy(t *testing.T) {
	tests := []struct {
		name string
		// Which room the troll is in ("" for no troll).
		trollRoom string
		wantRoom  string
		wantReply string
	}{
		{"an enemy in the same room", worldtest.StartID, worldtest.StartID, "It hits a Troll for %d damage."},
		{"an enemy in the next room", worldtest.NorthID, worldtest.NorthID, "In the next room, it hits a Troll for %d damage."},
		{"an enemy two rooms on", farID, farID, "2 rooms away, it hits a Troll for %d damage."},
		{"no enemy at all", "", farID, "It flies 2 rooms north without finding anything to hit, and clatters to the floor."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newThrowFixture(t)
			var troll *world.NPC
			if tt.trollRoom != "" {
				troll = f.addTroll(tt.trollRoom)
			}
			// Someone who can't be fought doesn't stop a spear.
			north := f.room(worldtest.NorthID)
			north.NPCs = append([]*world.NPC{{ID: "old-tom", Name: "Old Tom", RoomID: worldtest.NorthID}}, north.NPCs...)

			got := lines(f.run(t, f.throwWith(hitRoll, noCrit), "throw spear north"))
			want := []string{"You throw a spear north.", tt.wantReply}
			if troll != nil {
				want[1] = fmt.Sprintf(tt.wantReply, f.spearDamage())
			}
			if !slices.Equal(got, want) {
				t.Errorf("reply = %q, want %q", got, want)
			}
			if got := f.spearRoom(); got != tt.wantRoom {
				t.Errorf("the spear is in %q, want %q", got, tt.wantRoom)
			}
			if slices.Contains(f.player.Inventory, f.spear) {
				t.Error("the thrower still has the spear")
			}
			if troll != nil && troll.Stats.HitPoints != troll.Stats.MaxHitPoints-f.spearDamage() {
				t.Errorf("troll hit points = %d", troll.Stats.HitPoints)
			}
			// The troll never strikes back.
			if f.player.Stats.HitPoints != f.player.Stats.MaxHitPoints {
				t.Error("the thrower was hurt")
			}
			if got, want := f.player.Stats.Stamina, f.player.Stats.MaxStamina-combat.StaminaCost; got != want {
				t.Errorf("stamina = %d, want %d", got, want)
			}
		})
	}
}

func TestThrowStopsAtTheFirstEnemyEvenWhenItMisses(t *testing.T) {
	f := newThrowFixture(t)
	near, far := f.addTroll(worldtest.NorthID), f.addTroll(farID)

	got := f.run(t, f.throwWith(missRoll), "throw spear north")
	if want := "You throw a spear north.\r\nIn the next room, it flies past a Troll and clatters to the floor."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
	if f.spearRoom() != worldtest.NorthID {
		t.Errorf("the spear is in %q", f.spearRoom())
	}
	for _, troll := range []*world.NPC{near, far} {
		if troll.Stats.HitPoints != troll.Stats.MaxHitPoints {
			t.Error("a troll was hurt by a miss")
		}
	}
}

func TestThrowWithNowhereToGo(t *testing.T) {
	f := newThrowFixture(t)
	// No doorway east: it never leaves the room.
	want := "You throw a spear east.\r\nThere's nothing to hit and no way east: it clatters to the floor."
	if got := f.run(t, f.throwWith(), "throw spear east"); got != want {
		t.Errorf("throw east = %q, want %q", got, want)
	}
	if f.spearRoom() != worldtest.StartID {
		t.Errorf("the spear is in %q", f.spearRoom())
	}

	// Matching the direction ignores case, and the item can be more
	// than one word.
	f.room(worldtest.StartID).Items = nil
	f.player.Inventory = append(f.player.Inventory, f.spear)
	if got := f.run(t, f.throwWith(), "throw a spear NORTH"); !strings.Contains(got, "It flies 2 rooms north") {
		t.Errorf("throw NORTH = %q", got)
	}
}

func TestThrowDoesNotCircleForever(t *testing.T) {
	f := newThrowFixture(t)
	// north from the far room leads back to the start.
	far := f.room(farID)
	far.Doorways = append(far.Doorways, &world.Doorway{Direction: "north", To: worldtest.StartID})

	got := f.run(t, f.throwWith(), "throw spear north")
	if !strings.Contains(got, "It flies 2 rooms north") || f.spearRoom() != farID {
		t.Errorf("reply = %q, spear in %q", got, f.spearRoom())
	}
}

func TestAThrownSpearCanKill(t *testing.T) {
	f := newThrowFixture(t)
	troll := f.addTroll(worldtest.NorthID)
	troll.Stats.HitPoints = 1

	got := lines(f.run(t, f.throwWith(hitRoll, noCrit), "throw spear north"))
	want := []string{"You throw a spear north.", got[1], "A Troll collapses, dead.", "It drops a spiked club."}
	if !slices.Equal(got, want) {
		t.Errorf("reply = %q, want %q", got, want)
	}
	north := f.room(worldtest.NorthID)
	if len(north.NPCs) != 0 || len(north.Items) != 2 {
		t.Errorf("north room has %d NPCs and %d items, want none and the spear plus the club", len(north.NPCs), len(north.Items))
	}
}

func TestThrowTellsOtherPlayersAlongTheWay(t *testing.T) {
	f := newThrowFixture(t)
	f.addTroll(farID)
	want := map[string]string{
		worldtest.StartID: "alice throws a spear north.",
		worldtest.NorthID: "A spear flies through, heading north.",
		farID:             "A spear flies in and hits a Troll.",
	}
	watchers := map[string]<-chan string{}
	for roomID := range want {
		watcher := &world.Player{Name: "watcher", CurrentRoomID: roomID}
		if err := f.world.PlacePlayer(watcher); err != nil {
			t.Fatal(err)
		}
		watchers[roomID] = f.sessions.Create(watcher).Messages()
	}

	f.run(t, f.throwWith(hitRoll, noCrit), "throw spear north")

	for roomID, messages := range watchers {
		select {
		case got := <-messages:
			if got != want[roomID] {
				t.Errorf("in %s they saw %q, want %q", roomID, got, want[roomID])
			}
		default:
			t.Errorf("in %s they saw nothing, want %q", roomID, want[roomID])
		}
		if len(messages) != 0 {
			t.Errorf("in %s they saw more than one line", roomID)
		}
	}
	// The thrower gets it all in the reply, not a second time.
	if len(f.session.Messages()) != 0 {
		t.Error("the thrower was sent a message too")
	}
}

func TestThrowSavesTheThrower(t *testing.T) {
	f := newThrowFixture(t)
	f.run(t, f.throwWith(), "throw spear north")
	saved, err := f.repo.GetByName(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Stats == nil || saved.Stats.Stamina != f.player.Stats.MaxStamina-combat.StaminaCost {
		t.Errorf("saved stats = %+v", saved.Stats)
	}
}

func TestAThrownSpearRunsScripts(t *testing.T) {
	f := newThrowFixture(t)
	engine, commands := f.scripted(t)
	f.script(t, engine, "spear.lua", `
		local Spear = Item:extend()
		function Spear:on_hit(wielder, target) wielder:send("The spear hums.") end
		return Spear`)
	f.script(t, engine, "troll.lua", `
		local Troll = Monster:extend()
		function Troll:on_attacked(attacker)
			Monster.on_attacked(self, attacker)
			self:emote("looks around for " .. attacker.name .. ".")
		end
		return Troll`)
	f.spear.Script = "spear.lua"
	troll := f.addTroll(worldtest.NorthID)
	troll.Script = "troll.lua"
	throw := commands["throw"].(*ThrowCommand)
	throw.rng = combattest.NewRolls(hitRoll, noCrit)

	got := lines(f.run(t, throw, "throw spear north"))
	if !slices.Contains(got, "The spear hums.") {
		t.Errorf("reply %q is missing the spear's on_hit", got)
	}
	// The troll's on_attacked ran (its emote goes to its own room, where
	// the thrower isn't), and calling the base didn't get the thrower
	// hit from a room away.
	if f.player.Stats.HitPoints != f.player.Stats.MaxHitPoints {
		t.Error("the troll struck back at a thrower in another room")
	}
}
