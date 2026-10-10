package scripting

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mudengine/internal/session"
	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

var ctx = context.Background()

// fixture is a two-room world with a scripts directory, an engine, and
// two players logged in at worldtest.StartID: alice, whose commands
// the tests run, and bob, who is just standing there.
type fixture struct {
	t        *testing.T
	world    *world.World
	sessions *session.Registry
	engine   *Engine
	alice    *world.Player
	bob      *world.Player
	bobSess  *session.Session
	saved    []*world.Player
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, world: worldtest.TwoRooms(), sessions: session.NewRegistry()}
	f.world.ScriptsDir = t.TempDir()
	engine, err := New(f.world, f.sessions, Options{
		SavePlayer: func(_ context.Context, player *world.Player) error {
			f.saved = append(f.saved, player)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.Close)
	f.engine = engine
	f.alice = f.login("alice")
	f.bob = f.login("bob")
	for _, s := range f.sessions.InRoom(worldtest.StartID) {
		if s.Player == f.bob {
			f.bobSess = s
		}
	}
	return f
}

func (f *fixture) login(name string) *world.Player {
	f.t.Helper()
	stats := world.DefaultPlayerStats()
	player := &world.Player{Name: name, CurrentRoomID: worldtest.StartID, Stats: &stats}
	if err := f.world.PlacePlayer(player); err != nil {
		f.t.Fatal(err)
	}
	f.sessions.Create(player)
	return player
}

// script loads source as name, skipping Reload's admission: most
// tests' scripts are written for this fixture, not for whatever
// simulated traffic would throw at them.
func (f *fixture) script(name, source string) {
	f.t.Helper()
	loaded, err := f.engine.compile(ctx, name, source)
	if err != nil {
		f.t.Fatalf("compile(%s): %v", name, err)
	}
	f.engine.classes[name] = loaded
}

// reload writes name into the scripts directory and loads it the way
// the reload command does.
func (f *fixture) reload(name, source string) {
	f.t.Helper()
	f.write(name, source)
	if _, err := f.engine.Reload(ctx, name); err != nil {
		f.t.Fatalf("Reload(%s): %v", name, err)
	}
}

func (f *fixture) write(name, source string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.world.ScriptsDir, name), []byte(source), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// monster puts an enemy NPC running script in the starting room.
func (f *fixture) monster(script string) *world.NPC {
	npc := &world.NPC{
		ID: "rat", Name: "A Rat", Enemy: true, RoomID: worldtest.StartID, Script: script,
		Stats: &world.Stats{Strength: 2, Dexterity: 2, Luck: 2, HitPoints: 10, MaxHitPoints: 10, Stamina: 6, MaxStamina: 6},
	}
	room := worldtest.MustRoom(f.world, worldtest.StartID)
	room.NPCs = append(room.NPCs, npc)
	return npc
}

// heard is everything sent to bob's connection so far.
func (f *fixture) heard() []string {
	var lines []string
	for {
		select {
		case line := <-f.bobSess.Messages():
			lines = append(lines, line)
		default:
			return lines
		}
	}
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %q, want %q", what, got, want)
	}
}

func TestSandboxHidesEverythingOutsideTheGame(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"io", "os", "debug", "package", "require", "load", "loadstring", "loadfile", "dofile", "coroutine"} {
		f.write("probe.lua", "if "+name+" ~= nil then error('"+name+" is reachable') end\nreturn Monster:extend()")
		if _, err := f.engine.Reload(ctx, "probe.lua"); err != nil {
			t.Error(err)
		}
	}
}

func TestStringRepIsCapped(t *testing.T) {
	f := newFixture(t)
	f.write("big.lua", `local s = string.rep("x", 2000000) return Monster:extend()`)
	if _, err := f.engine.Reload(ctx, "big.lua"); err == nil || !strings.Contains(err.Error(), "string.rep") {
		t.Errorf("Reload = %v, want a string.rep error", err)
	}
	f.reload("small.lua", `assert(#string.rep("ab", 3) == 6) assert(("x"):rep(0) == "") return Monster:extend()`)
}

func TestReloadRejectsWhatIsNotAClass(t *testing.T) {
	f := newFixture(t)
	tests := []struct{ name, source, want string }{
		{"syntax", "local Rat = Monster:extend(\nreturn Rat", "syntax.lua"},
		{"runtime", "local Rat = Monster:extend()\nRat.x = nil + 1\nreturn Rat", "runtime.lua:2"},
		{"nothing", "local Rat = Monster:extend()", "must end by returning its class"},
		{"number", "return 7", "must end by returning its class"},
		{"plain table", "return {}", "Monster:extend() or Item:extend()"},
		{"base class", "return Monster", "Monster:extend() or Item:extend()"},
	}
	for _, tt := range tests {
		file := tt.name[:strings.IndexAny(tt.name+" ", " ")] + ".lua"
		f.write(file, tt.source)
		_, err := f.engine.Reload(ctx, file)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Reload = %v, want an error containing %q", tt.name, err, tt.want)
		}
	}
	if _, err := f.engine.Reload(ctx, "missing.lua"); err == nil || !strings.Contains(err.Error(), "no such script") {
		t.Errorf("missing file: Reload = %v", err)
	}
	if loaded := f.engine.Loaded(); len(loaded) != 0 {
		t.Errorf("Loaded() = %v, want nothing", loaded)
	}
}

func TestReloadReportsTheKind(t *testing.T) {
	f := newFixture(t)
	f.write("m.lua", "return Monster:extend()")
	f.write("i.lua", "return Item:extend():extend()")
	if kind, err := f.engine.Reload(ctx, "m.lua"); err != nil || kind != Monster {
		t.Errorf("m.lua: %v, %v", kind, err)
	}
	if kind, err := f.engine.Reload(ctx, "i.lua"); err != nil || kind != Item {
		t.Errorf("i.lua: %v, %v", kind, err)
	}
	equal(t, "Loaded()", f.engine.Loaded(), []string{"i.lua", "m.lua"})
}

func TestScriptName(t *testing.T) {
	for arg, want := range map[string]string{"rat": "rat.lua", " rat.lua ": "rat.lua", "swamp-dragon": "swamp-dragon.lua"} {
		if got, err := ScriptName(arg); err != nil || got != want {
			t.Errorf("ScriptName(%q) = %q, %v", arg, got, err)
		}
	}
	for _, arg := range []string{"", "  ", "../rat", "a/b.lua", ".lua", "two words"} {
		if got, err := ScriptName(arg); err == nil {
			t.Errorf("ScriptName(%q) = %q, want an error", arg, got)
		}
	}
}

func TestHookOutputGoesToTheActorAsRepliesAndToOthersAsMessages(t *testing.T) {
	f := newFixture(t)
	f.script("rat.lua", `
		local Rat = Monster:extend()
		function Rat:on_enter(player)
			self:emote("squeaks at " .. player.name .. ".")
			self:say("Squeak.")
			player:send("Only you see this.")
			self.room:send("Line one.\nLine two.")
		end
		return Rat`)
	f.monster("rat.lua")

	want := []string{"A Rat squeaks at alice.", "A Rat says: Squeak.", "Only you see this.", "Line one.\r\nLine two."}
	equal(t, "replies", f.engine.PlayerEntered(ctx, f.alice), want)
	equal(t, "bob heard", f.heard(), []string{want[0], want[1], want[3]})
}

func TestOnSayGetsTheText(t *testing.T) {
	f := newFixture(t)
	f.script("rat.lua", `
		local Rat = Monster:extend()
		function Rat:on_say(player, text)
			if text:lower():find("cheese") then self:emote("looks up.") end
		end
		return Rat`)
	f.monster("rat.lua")
	equal(t, "no cheese", f.engine.PlayerSaid(ctx, f.alice, "hello"), nil)
	equal(t, "cheese", f.engine.PlayerSaid(ctx, f.alice, "Any CHEESE?"), []string{"A Rat looks up."})
}

func TestPropertiesAndHandles(t *testing.T) {
	f := newFixture(t)
	f.script("rat.lua", `
		local Rat = Monster:extend()
		function Rat:on_enter(player)
			assert(self.id == "rat" and self.name == "A Rat")
			assert(self.room.id == "start" and self.room == player.room)
			assert(self.stats.hit_points == 10 and self.stats.max_hit_points == 10)
			assert(player.stats.strength == 5)
			assert(self.nonsense == nil and player.nonsense == nil and self.room.nonsense == nil)
			local players = self.room:players()
			assert(#players == 2 and players[1] == player and players[2] ~= player)
			assert(players[2].name == "bob")
			assert(Rat.super == Monster)
			player:send("ok")
		end
		return Rat`)
	f.monster("rat.lua")
	equal(t, "replies", f.engine.PlayerEntered(ctx, f.alice), []string{"ok"})
}

func TestStateIsPerInstanceAndLocalsArePerScript(t *testing.T) {
	f := newFixture(t)
	f.script("rat.lua", `
		local Rat = Monster:extend()
		local total = 0
		function Rat:on_enter(player)
			self.seen = (self.seen or 0) + 1
			total = total + 1
			player:send(self.seen .. " of " .. total)
		end
		return Rat`)
	f.monster("rat.lua")
	equal(t, "one rat", f.engine.PlayerEntered(ctx, f.alice), []string{"1 of 1"})
	f.monster("rat.lua")
	equal(t, "two rats", f.engine.PlayerEntered(ctx, f.alice), []string{"2 of 2", "1 of 3"})
}

func TestGlobalsDoNotLeakBetweenScripts(t *testing.T) {
	f := newFixture(t)
	f.script("a.lua", "leaked = 'from a'\nreturn Monster:extend()")
	f.script("b.lua", "assert(leaked == nil)\nreturn Monster:extend()")
}

func TestOnAttacked(t *testing.T) {
	tests := []struct {
		name, source string
		wantStrikes  int
		wantReplies  []string
	}{
		{"no override strikes back", `return Monster:extend()`, 1, nil},
		{"calling the base strikes back", `
			local Rat = Monster:extend()
			function Rat:on_attacked(attacker)
				Monster.on_attacked(self, attacker)
				Monster.on_attacked(self, attacker)
				self:emote("hisses at " .. attacker.name .. ".")
			end
			return Rat`, 1, []string{"A Rat hisses at alice."}},
		{"not calling the base doesn't", `
			local Rat = Monster:extend()
			function Rat:on_attacked(attacker) self:emote("cowers.") end
			return Rat`, 0, []string{"A Rat cowers."}},
		{"failing before the base still strikes back", `
			local Rat = Monster:extend()
			function Rat:on_attacked(attacker) error("oops") end
			return Rat`, 1, nil},
		{"failing after the base strikes back once", `
			local Rat = Monster:extend()
			function Rat:on_attacked(attacker)
				Monster.on_attacked(self, attacker)
				error("oops")
			end
			return Rat`, 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.script("rat.lua", tt.source)
			rat := f.monster("rat.lua")

			strikes := 0
			replies := f.engine.NPCAttacked(ctx, rat, f.alice, func() { strikes++ })
			if strikes != tt.wantStrikes {
				t.Errorf("strikes = %d, want %d", strikes, tt.wantStrikes)
			}
			equal(t, "replies", replies, tt.wantReplies)
		})
	}
}

func TestOnDeathCanSpawnAndDamage(t *testing.T) {
	f := newFixture(t)
	f.world.NPCs["rat"] = &world.NPC{ID: "rat", Name: "A Rat", Script: "rat.lua"}
	f.script("rat.lua", `
		local Rat = Monster:extend()
		function Rat:on_death(killer)
			local another = self.room:spawn("rat")
			assert(another.name == "A Rat" and another ~= self)
			for _, player in ipairs(self.room:players()) do
				player:damage(player.name == "bob" and 1000 or 3)
			end
			killer:damage(-5)
		end
		return Rat`)
	rat := f.monster("rat.lua")
	rat.Stats.HitPoints = 0

	replies := f.engine.NPCDied(ctx, rat, f.alice)
	equal(t, "replies", replies, nil)
	room := worldtest.MustRoom(f.world, worldtest.StartID)
	if len(room.NPCs) != 2 || room.NPCs[1].ID != "rat" || room.NPCs[1] == rat {
		t.Errorf("room NPCs = %v, want the dead rat and a fresh one", room.NPCs)
	}
	if got, want := f.alice.Stats.HitPoints, f.alice.Stats.MaxHitPoints-3; got != want {
		t.Errorf("alice has %d hit points, want %d", got, want)
	}
	// bob died: revived in the starting room (where he already was),
	// fully healed, and told so.
	if f.bob.Stats.HitPoints != f.bob.Stats.MaxHitPoints || f.bob.CurrentRoomID != worldtest.StartID {
		t.Errorf("bob = %+v in %s", f.bob.Stats, f.bob.CurrentRoomID)
	}
	heard := f.heard()
	if len(heard) != 1 || !strings.HasPrefix(heard[0], "You collapse, dead.") || !strings.Contains(heard[0], "["+worldtest.StartID+"]") {
		t.Errorf("bob heard %q", heard)
	}
	if !slices.Equal(f.saved, []*world.Player{f.alice, f.bob}) {
		t.Errorf("saved %d players, want alice then bob", len(f.saved))
	}
	if _, kept := f.engine.instances[rat]; kept {
		t.Error("the dead rat's instance state wasn't dropped")
	}
}

func TestSpawnStopsWhenTheRoomIsFullAndRejectsUnknownIDs(t *testing.T) {
	f := newFixture(t)
	f.world.NPCs["rat"] = &world.NPC{ID: "rat", Name: "A Rat"}
	f.script("rat.lua", `
		local Rat = Monster:extend()
		function Rat:on_say(player, text)
			if text == "unknown" then self.room:spawn("no-such-npc") end
			local count = 0
			for i = 1, 100 do
				if self.room:spawn("rat") then count = count + 1 end
			end
			player:send(tostring(count))
		end
		return Rat`)
	f.monster("rat.lua")
	equal(t, "spawned", f.engine.PlayerSaid(ctx, f.alice, "go"), []string{"19"})
	if got := len(worldtest.MustRoom(f.world, worldtest.StartID).NPCs); got != MaxRoomNPCs {
		t.Errorf("room has %d NPCs, want %d", got, MaxRoomNPCs)
	}
	equal(t, "unknown id", f.engine.PlayerSaid(ctx, f.alice, "unknown"), nil)
	if err := f.engine.lastErr; err == nil || !strings.Contains(err.Error(), `no NPC is defined with id "no-such-npc"`) {
		t.Errorf("lastErr = %v", err)
	}
}

func TestItemHooks(t *testing.T) {
	f := newFixture(t)
	f.script("sword.lua", `
		local Sword = Item:extend()
		function Sword:on_pickup(player)
			self.swings = 0
			player:send(self.name .. " (" .. self.damage .. ") picked up in " .. self.room.id)
		end
		function Sword:on_hit(wielder, target)
			self.swings = self.swings + 1
			wielder.room:send(wielder.name .. " hits " .. target.name .. " #" .. self.swings)
		end
		function Sword:on_drop(player) player.room:send("clang") end
		return Sword`)
	sword := &world.Item{ID: "sword", Name: "a sword", Weapon: &world.Weapon{Damage: 4}, Script: "sword.lua"}
	plain := &world.Item{ID: "rock", Name: "a rock"}
	rat := f.monster("")
	f.alice.Inventory = []*world.Item{sword}

	equal(t, "pickup", f.engine.ItemPickedUp(ctx, sword, f.alice), []string{"a sword (4) picked up in start"})
	equal(t, "player hits", f.engine.ItemHit(ctx, sword, f.alice, rat, f.alice), []string{"alice hits A Rat #1"})
	equal(t, "npc hits", f.engine.ItemHit(ctx, sword, rat, f.alice, f.alice), []string{"A Rat hits alice #2"})
	equal(t, "drop", f.engine.ItemDropped(ctx, sword, f.alice), []string{"clang"})
	equal(t, "unscripted item", f.engine.ItemPickedUp(ctx, plain, f.alice), nil)
	if got := len(f.heard()); got != 3 {
		t.Errorf("bob heard %d lines, want 3 (both hits and the drop)", got)
	}
}

func TestReloadSwapsBehaviourAndKeepsState(t *testing.T) {
	f := newFixture(t)
	version := func(word string) string {
		return `
		local Rat = Monster:extend()
		function Rat:on_enter(player)
			self.seen = (self.seen or 0) + 1
			player:send("` + word + ` " .. self.seen)
		end
		return Rat`
	}
	f.reload("rat.lua", version("old"))
	f.monster("rat.lua")
	equal(t, "before", f.engine.PlayerEntered(ctx, f.alice), []string{"old 1"})

	f.reload("rat.lua", version("new"))
	equal(t, "after reload", f.engine.PlayerEntered(ctx, f.alice), []string{"new 2"})

	// A broken new version changes nothing.
	f.write("rat.lua", "this is not lua")
	if _, err := f.engine.Reload(ctx, "rat.lua"); err == nil {
		t.Fatal("Reload accepted a syntax error")
	}
	equal(t, "after a bad reload", f.engine.PlayerEntered(ctx, f.alice), []string{"new 3"})

	if !f.engine.Unload("rat.lua") || f.engine.Unload("rat.lua") {
		t.Error("Unload should report true once, then false")
	}
	equal(t, "unloaded", f.engine.PlayerEntered(ctx, f.alice), nil)

	f.reload("rat.lua", version("back"))
	equal(t, "loaded again", f.engine.PlayerEntered(ctx, f.alice), []string{"back 4"})
}

func TestARunawayHookIsStoppedAndTheEngineCarriesOn(t *testing.T) {
	f := newFixture(t)
	f.engine.timeout = 20 * time.Millisecond
	f.script("rat.lua", `
		local Rat = Monster:extend()
		function Rat:on_say(player, text)
			if text == "spin" then while true do end end
			player:send("fine")
		end
		return Rat`)
	f.monster("rat.lua")

	start := time.Now()
	equal(t, "runaway", f.engine.PlayerSaid(ctx, f.alice, "spin"), nil)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("the runaway hook ran for %v", elapsed)
	}
	if err := f.engine.lastErr; err == nil || !strings.Contains(err.Error(), "ran for longer than") {
		t.Errorf("lastErr = %v", err)
	}
	equal(t, "afterwards", f.engine.PlayerSaid(ctx, f.alice, "hello"), []string{"fine"})
}

func TestAScriptIsDisabledAfterRepeatedFailuresUntilReloaded(t *testing.T) {
	f := newFixture(t)
	source := func(broken bool) string {
		body := `player:send("fine")`
		if broken {
			body = `if text == "boom" then error("boom") end ` + body
		}
		return "local Rat = Monster:extend()\nfunction Rat:on_say(player, text) " + body + " end\nreturn Rat"
	}
	f.reload("rat.lua", source(true))
	f.monster("rat.lua")

	// A success in between resets the count.
	for range MaxFailures - 1 {
		f.engine.PlayerSaid(ctx, f.alice, "boom")
	}
	equal(t, "still enabled", f.engine.PlayerSaid(ctx, f.alice, "hi"), []string{"fine"})
	for range MaxFailures {
		f.engine.PlayerSaid(ctx, f.alice, "boom")
	}
	equal(t, "disabled", f.engine.PlayerSaid(ctx, f.alice, "hi"), nil)

	f.reload("rat.lua", source(false))
	equal(t, "reloaded", f.engine.PlayerSaid(ctx, f.alice, "hi"), []string{"fine"})
}

func TestTickRunsOnTickOnLiveScriptedNPCs(t *testing.T) {
	f := newFixture(t)
	f.script("rat.lua", `
		local Rat = Monster:extend()
		function Rat:on_tick() self:emote("scratches.") end
		return Rat`)
	f.monster("rat.lua")
	f.monster("")
	dead := f.monster("rat.lua")
	if _, err := f.world.KillNPC(dead); err != nil {
		t.Fatal(err)
	}

	f.engine.Tick(ctx)
	equal(t, "bob heard", f.heard(), []string{"A Rat scratches."})
}

func TestMethodsCalledWithADotExplainThemselves(t *testing.T) {
	f := newFixture(t)
	f.script("rat.lua", `
		local Rat = Monster:extend()
		function Rat:on_say(player, text)
			if text == "say" then self.say("hello") else player.send("hello") end
		end
		return Rat`)
	f.monster("rat.lua")
	for _, text := range []string{"say", "send"} {
		f.engine.PlayerSaid(ctx, f.alice, text)
		if err := f.engine.lastErr; err == nil || !strings.Contains(err.Error(), "call methods with ':'") {
			t.Errorf("%s: lastErr = %v", text, err)
		}
	}
}

func TestANilEngineBehavesLikeAWorldWithNoScripts(t *testing.T) {
	var e *Engine
	player := &world.Player{Name: "alice"}
	npc := &world.NPC{Name: "A Rat", Script: "rat.lua"}
	item := &world.Item{Name: "a sword", Script: "sword.lua"}

	strikes := 0
	if lines := e.NPCAttacked(ctx, npc, player, func() { strikes++ }); lines != nil || strikes != 1 {
		t.Errorf("NPCAttacked = %v, %d strikes", lines, strikes)
	}
	for what, lines := range map[string][]string{
		"PlayerEntered": e.PlayerEntered(ctx, player),
		"PlayerSaid":    e.PlayerSaid(ctx, player, "hi"),
		"NPCDied":       e.NPCDied(ctx, npc, player),
		"ItemPickedUp":  e.ItemPickedUp(ctx, item, player),
		"ItemDropped":   e.ItemDropped(ctx, item, player),
		"ItemHit":       e.ItemHit(ctx, item, player, npc, player),
	} {
		if lines != nil {
			t.Errorf("%s = %v", what, lines)
		}
	}
	e.Tick(ctx)
	e.Close()
	if e.Unload("rat.lua") || e.Loaded() != nil {
		t.Error("a nil engine has nothing loaded")
	}
	if _, err := e.Reload(ctx, "rat.lua"); err == nil {
		t.Error("Reload on a nil engine should fail")
	}
	if loaded, failed := e.LoadAll(ctx); loaded != 0 || failed != 0 {
		t.Errorf("LoadAll = %d, %d", loaded, failed)
	}
}

func TestLoadAllLoadsWhatTheWorldRefersToAndSkipsWhatFails(t *testing.T) {
	f := newFixture(t)
	f.write("rat.lua", "return Monster:extend()")
	f.write("sword.lua", "return Item:extend()")
	f.write("unused.lua", "return Monster:extend()")
	f.write("broken.lua", "return 7")
	f.world.NPCs["rat"] = &world.NPC{ID: "rat", Script: "rat.lua",
		Inventory: []*world.Item{{ID: "sword", Script: "sword.lua"}}}
	room := worldtest.MustRoom(f.world, worldtest.NorthID)
	room.Items = append(room.Items, &world.Item{ID: "rock", Script: "broken.lua"}, &world.Item{ID: "twig"})
	room.NPCs = append(room.NPCs, &world.NPC{ID: "ghost", Script: "missing.lua"})

	equal(t, "Referenced", Referenced(f.world), []string{"broken.lua", "missing.lua", "rat.lua", "sword.lua"})
	if loaded, failed := f.engine.LoadAll(ctx); loaded != 2 || failed != 2 {
		t.Errorf("LoadAll = %d loaded, %d failed; want 2 and 2", loaded, failed)
	}
	equal(t, "Loaded", f.engine.Loaded(), []string{"rat.lua", "sword.lua"})
}
