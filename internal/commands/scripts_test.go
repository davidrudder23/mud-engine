package commands

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mudengine/internal/combat/combattest"
	"mudengine/internal/scripting"
	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

// scripted gives the fixture a scripts directory and an engine, and
// returns every command wired to it, by name.
func (f *fixture) scripted(t *testing.T) (*scripting.Engine, map[string]Command) {
	t.Helper()
	f.world.ScriptsDir = t.TempDir()
	engine, err := scripting.New(f.world, f.sessions, scripting.Options{SavePlayer: f.repo.Save})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.Close)
	byName := map[string]Command{}
	for _, c := range BuildAll(f.sessions, f.world, f.repo, nil, engine) {
		byName[c.Name()] = c
	}
	return engine, byName
}

// script writes name into the scripts directory and loads it.
func (f *fixture) script(t *testing.T, engine *scripting.Engine, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.world.ScriptsDir, name), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Reload(context.Background(), name); err != nil {
		t.Fatal(err)
	}
}

func lines(reply string) []string {
	return strings.Split(reply, "\r\n")
}

func TestScriptCommandsOnlyExistWithAnEngine(t *testing.T) {
	f := newFixture(t)
	for _, c := range BuildAll(f.sessions, f.world, f.repo, nil, nil) {
		if c.Name() == "reload" || c.Name() == "unload" {
			t.Errorf("BuildAll without an engine includes %q", c.Name())
		}
	}
	_, withEngine := f.scripted(t)
	if withEngine["reload"] == nil || withEngine["unload"] == nil {
		t.Error("BuildAll with an engine should include reload and unload")
	}
}

func TestGoAndSayRunTheRoomsScripts(t *testing.T) {
	f := newFixture(t)
	engine, commands := f.scripted(t)
	f.script(t, engine, "rat.lua", `
		local Rat = Monster:extend()
		function Rat:on_enter(player) self:emote("bares its teeth at " .. player.name .. ".") end
		function Rat:on_say(player, text) self:say("You said " .. text) end
		return Rat`)
	rat := f.addTroll(worldtest.NorthID)
	rat.Name, rat.Script = "A Rat", "rat.lua"

	// The room as it was walked into, then the rat's reaction.
	got := lines(f.run(t, commands["go"], "go north"))
	if got[0] != "["+worldtest.NorthID+"]" || got[len(got)-1] != "A Rat bares its teeth at alice." {
		t.Errorf("go = %q", got)
	}
	if got := f.run(t, commands["say"], "say Hello there"); got != "You say: Hello there\r\nA Rat says: You said Hello there" {
		t.Errorf("say = %q", got)
	}
	// Nobody scripted back in the first room.
	if got := lines(f.run(t, commands["go"], "go south")); got[len(got)-1] == "A Rat bares its teeth at alice." {
		t.Errorf("go south = %q", got)
	}
}

func TestPickUpAndDropRunTheItemsScript(t *testing.T) {
	f := newFixture(t)
	engine, commands := f.scripted(t)
	f.script(t, engine, "sword.lua", `
		local Sword = Item:extend()
		function Sword:on_pickup(player) player:send("The sword sighs.") end
		function Sword:on_drop(player) player.room:send("The sword clatters.") end
		return Sword`)
	room := f.room(worldtest.StartID)
	room.Items = append(room.Items, &world.Item{ID: "sword", Name: "a sword", Script: "sword.lua"}, &world.Item{ID: "rock", Name: "a rock"})

	if got := f.run(t, commands["pick up"], "pick up sword"); got != "You pick up a sword.\r\nThe sword sighs." {
		t.Errorf("pick up = %q", got)
	}
	if got := f.run(t, commands["drop"], "drop sword"); got != "You drop a sword.\r\nThe sword clatters." {
		t.Errorf("drop = %q", got)
	}
	if got := f.run(t, commands["pick up"], "pick up rock"); got != "You pick up a rock." {
		t.Errorf("pick up rock = %q", got)
	}
}

// attackIn is the wired attack command with fixed rolls.
func attackIn(commands map[string]Command, rolls ...int) *AttackCommand {
	attack := commands["attack"].(*AttackCommand)
	attack.rng = combattest.NewRolls(rolls...)
	return attack
}

func TestAScriptedEnemyDecidesWhetherToStrikeBack(t *testing.T) {
	tests := []struct {
		name, onAttacked string
		wantStruck       bool
		wantSaid         string
	}{
		{"no override", ``, true, ""},
		{"calls the base", `function Troll:on_attacked(attacker)
			Monster.on_attacked(self, attacker)
			self:emote("roars.")
		end`, true, "A Troll roars."},
		{"doesn't call the base", `function Troll:on_attacked(attacker) self:emote("cowers.") end`, false, "A Troll cowers."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			engine, commands := f.scripted(t)
			f.script(t, engine, "troll.lua", "local Troll = Monster:extend()\n"+tt.onAttacked+"\nreturn Troll")
			troll := f.addTroll(worldtest.StartID)
			troll.Script = "troll.lua"

			got := lines(f.run(t, attackIn(commands, hitRoll, noCrit, hitRoll, noCrit), "attack troll"))
			struck := slices.ContainsFunc(got, func(line string) bool { return strings.HasPrefix(line, "A Troll hits you") })
			if struck != tt.wantStruck {
				t.Errorf("struck back = %v, want %v, in %q", struck, tt.wantStruck, got)
			}
			hurt := f.player.Stats.HitPoints < f.player.Stats.MaxHitPoints
			if hurt != tt.wantStruck {
				t.Errorf("player hurt = %v, want %v", hurt, tt.wantStruck)
			}
			if tt.wantSaid != "" && !slices.Contains(got, tt.wantSaid) {
				t.Errorf("reply %q doesn't include %q", got, tt.wantSaid)
			}
		})
	}
}

func TestAScriptedEnemyDiesItsOwnWay(t *testing.T) {
	f := newFixture(t)
	engine, commands := f.scripted(t)
	f.script(t, engine, "troll.lua", `
		local Troll = Monster:extend()
		function Troll:on_death(killer)
			self.room:send("The troll crumbles into gravel.")
			killer:damage(2)
		end
		return Troll`)
	troll := f.addTroll(worldtest.StartID)
	troll.Script = "troll.lua"
	troll.Stats.HitPoints = 1

	got := lines(f.run(t, attackIn(commands, hitRoll, noCrit), "attack troll"))
	want := []string{
		got[0], // the blow itself
		"A Troll collapses, dead.",
		"The troll crumbles into gravel.",
		"It drops a spiked club.",
	}
	if !slices.Equal(got, want) {
		t.Errorf("reply = %q, want %q", got, want)
	}
	if len(f.room(worldtest.StartID).NPCs) != 0 {
		t.Error("the troll is still in the room")
	}
	if got, want := f.player.Stats.HitPoints, f.player.Stats.MaxHitPoints-2; got != want {
		t.Errorf("player has %d hit points, want %d", got, want)
	}
}

func TestAScriptedWeaponHearsEveryBlowLandedWithIt(t *testing.T) {
	f := newFixture(t)
	engine, commands := f.scripted(t)
	f.script(t, engine, "weapon.lua", `
		local Weapon = Item:extend()
		function Weapon:on_hit(wielder, target)
			wielder.room:send(self.name .. ": " .. wielder.name .. " -> " .. target.name)
		end
		return Weapon`)
	troll := f.addTroll(worldtest.StartID)
	troll.Inventory[0].Script = "weapon.lua"
	f.player.Inventory = []*world.Item{{ID: "sword", Name: "a sword", Weapon: &world.Weapon{Damage: 1}, Script: "weapon.lua"}}

	got := lines(f.run(t, attackIn(commands, hitRoll, noCrit, hitRoll, noCrit), "attack troll"))
	for _, want := range []string{"a sword: alice -> A Troll", "a spiked club: A Troll -> alice"} {
		if !slices.Contains(got, want) {
			t.Errorf("reply %q doesn't include %q", got, want)
		}
	}
	// A miss isn't a hit.
	got = lines(f.run(t, attackIn(commands, missRoll, missRoll), "attack troll"))
	if slices.ContainsFunc(got, func(line string) bool { return strings.Contains(line, "->") }) {
		t.Errorf("on_hit ran on a miss: %q", got)
	}
}

func TestReloadAndUnload(t *testing.T) {
	f := newFixture(t)
	engine, commands := f.scripted(t)
	reload, unload := commands["reload"], commands["unload"]
	write := func(source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(f.world.ScriptsDir, "rat.lua"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("return Monster:extend()")

	// alice isn't a wizzard until MUD_WIZZARDS says so.
	for _, c := range []Command{reload, unload} {
		if got := f.run(t, c, c.Name()+" rat"); got != "Only a wizzard can do that." {
			t.Errorf("%s as a non-wizzard = %q", c.Name(), got)
		}
	}
	if len(engine.Loaded()) != 0 {
		t.Fatal("a non-wizzard loaded a script")
	}
	t.Setenv("MUD_WIZZARDS", "Rincewind, Alice")

	tests := []struct {
		command    Command
		line, want string
	}{
		{reload, "reload", "Reload which script?"},
		{reload, "reload ../rat", "'../rat' isn't a script name."},
		{reload, "reload cat", "Not loaded: no such script: cat.lua"},
		{unload, "unload rat", "rat.lua isn't loaded."},
		{reload, "reload rat", "Loaded rat.lua (a monster)."},
		{reload, "reload rat.lua", "Loaded rat.lua (a monster)."},
		{unload, "unload", "Unload which script?"},
		{unload, "unload rat", "Unloaded rat.lua."},
		{unload, "unload rat", "rat.lua isn't loaded."},
	}
	for _, tt := range tests {
		if got := f.run(t, tt.command, tt.line); got != tt.want {
			t.Errorf("%q = %q, want %q", tt.line, got, tt.want)
		}
	}

	// A version that doesn't compile, or doesn't survive admission, is
	// reported and leaves the running one alone.
	write("return Monster:extend()")
	f.run(t, reload, "reload rat")
	for source, want := range map[string]string{
		"return Monster:extend(": "Not loaded: rat.lua",
		"local R = Monster:extend()\nfunction R:on_tick() error('no') end\nreturn R": "Not loaded: on_tick failed under simulated traffic: rat.lua:2: no",
	} {
		write(source)
		if got := f.run(t, reload, "reload rat"); !strings.HasPrefix(got, want) {
			t.Errorf("reload of %q = %q, want it to start %q", source, got, want)
		}
	}
	if !slices.Equal(engine.Loaded(), []string{"rat.lua"}) {
		t.Errorf("Loaded() = %v, want the earlier rat.lua still there", engine.Loaded())
	}

	write("return Item:extend()")
	if got := f.run(t, reload, "reload rat"); got != "Loaded rat.lua (an item)." {
		t.Errorf("reload = %q", got)
	}
}
