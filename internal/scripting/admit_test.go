package scripting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mudengine/internal/world"
)

// These measure the whole process's memory, so none of them (and
// nothing else in this package) may call t.Parallel.

func TestAdmit(t *testing.T) {
	tests := []struct {
		name, source string
		// "" to pass; otherwise what the error has to mention.
		want string
	}{
		{"a monster that does nothing", `return Monster:extend()`, ""},
		{"an item that does nothing", `return Item:extend()`, ""},
		{"a monster with bounded state", `
			local Rat = Monster:extend()
			function Rat:on_enter(player)
				self.last = player.name
				self.count = (self.count or 0) + 1
				self:emote("looks at " .. player.name .. " for the " .. self.count .. "th time.")
			end
			function Rat:on_tick() self:say(string.rep("squeak ", 10)) end
			return Rat`, ""},
		{"a hook that fails", `
			local Rat = Monster:extend()
			function Rat:on_say(player, text) self:emote(nil .. text) end
			return Rat`, "on_say failed under simulated traffic"},
		{"a hook that only fails when the monster is hurt", `
			local Rat = Monster:extend()
			function Rat:on_attacked(attacker)
				if self.stats.hit_points * 2 < self.stats.max_hit_points then error("ouch") end
			end
			return Rat`, "on_attacked failed under simulated traffic"},
		{"a death hook that fails", `
			local Rat = Monster:extend()
			function Rat:on_death(killer) self.room:spawn("no-such-npc") end
			return Rat`, "on_death failed under simulated traffic"},
		{"a hook that never returns", `
			local Rat = Monster:extend()
			function Rat:on_tick() while true do end end
			return Rat`, "ran for longer than"},
		{"a script that never finishes loading", `while true do end`, "ran for longer than"},
		{"a monster that remembers everyone forever", `
			local Rat = Monster:extend()
			function Rat:on_enter(player)
				self.seen = self.seen or {}
				table.insert(self.seen, player.name)
			end
			return Rat`, "keeps holding on to more memory"},
		{"an item that remembers every blow forever", `
			local Sword = Item:extend()
			local log = {}
			function Sword:on_hit(wielder, target) log[#log + 1] = {wielder.name, target.name} end
			return Sword`, "keeps holding on to more memory"},
		{"a string that doubles every time", `
			local Rat = Monster:extend()
			function Rat:on_enter(player)
				self.s = (self.s or "octarine") .. (self.s or "octarine")
			end
			return Rat`, "uses too much memory"},
		{"a hook that churns through memory", `
			local Rat = Monster:extend()
			function Rat:on_tick()
				local t = {}
				for i = 1, 20000 do t[i] = "x" .. i end
			end
			return Rat`, "uses too much memory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Admit(ctx, "subject.lua", tt.source, map[string]*world.NPC{})
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("Admit = %v, want it to pass", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("Admit = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

func TestAdmitTestsAMonsterAsTheNPCThatUsesIt(t *testing.T) {
	definitions := map[string]*world.NPC{
		"dragon": {ID: "dragon", Name: "A Dragon", Script: "subject.lua",
			Stats: &world.Stats{HitPoints: 7, MaxHitPoints: 7}},
	}
	source := `
		local Dragon = Monster:extend()
		function Dragon:on_enter(player)
			assert(self.name == "A Dragon" and self.stats.max_hit_points == 7, "tested as " .. self.name)
		end
		function Dragon:on_death(killer) self.room:spawn("dragon") end
		return Dragon`
	if err := Admit(ctx, "subject.lua", source, definitions); err != nil {
		t.Error(err)
	}
	if hp := definitions["dragon"].Stats.HitPoints; hp != 7 {
		t.Errorf("the definition's hit points are now %d", hp)
	}
}

func TestAdmitRejectsAScriptThatIsTooSlow(t *testing.T) {
	// Each hook is well inside its own deadline; together they aren't
	// inside the run's.
	admitTimeout = 5 * time.Millisecond
	defer func() { admitTimeout = AdmitTimeout }()
	source := `
		local Rat = Monster:extend()
		function Rat:on_tick() for i = 1, 3000 do end end
		return Rat`
	err := Admit(ctx, "subject.lua", source, map[string]*world.NPC{})
	if err == nil || !strings.Contains(err.Error(), "too slow") {
		t.Errorf("Admit = %v, want a too-slow error", err)
	}
}

// Every script shipped in world/ has to be one the server would load:
// this is the same check `reload` runs, over the real world.
func TestEveryWorldScriptIsAdmitted(t *testing.T) {
	worldDir := filepath.Join("..", "..", "world")
	w, err := world.Load(worldDir)
	if err != nil {
		t.Fatal(err)
	}
	referenced := Referenced(w)
	if len(referenced) == 0 {
		t.Fatal("the world refers to no scripts")
	}
	for _, name := range referenced {
		source, err := os.ReadFile(filepath.Join(w.ScriptsDir, name))
		if err != nil {
			t.Errorf("%s is referenced by the world but can't be read: %v", name, err)
			continue
		}
		if err := Admit(ctx, name, string(source), w.NPCs); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	// And nothing sits in world/scripts unused, where it would never
	// be checked.
	files, err := filepath.Glob(filepath.Join(w.ScriptsDir, "*.lua"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		used := false
		for _, name := range referenced {
			used = used || name == filepath.Base(file)
		}
		if !used {
			t.Errorf("%s isn't used by any NPC or item in the world", file)
		}
	}
}
