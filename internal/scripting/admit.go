package scripting

// The admission harness: what a script has to get through before it
// goes live. gopher-lua can't cap a script's memory while it runs, so
// instead a new version is run first, here, against simulated traffic
// in a scratch engine and a throwaway world, and rejected if it
// misbehaves.
//
// What it catches is limited to what the traffic exercises: a hook
// that only goes wrong when a player says one particular word gets
// through. And its memory numbers are the whole process's, not the
// script's, so the budgets are generous and it must not run alongside
// anything allocating heavily (tests that call it can't be parallel).

import (
	"context"
	"fmt"
	"runtime"
	"runtime/metrics"
	"time"

	"mudengine/internal/session"
	"mudengine/internal/world"
)

const (
	// How the traffic is split up. A round is a handful of hook calls
	// (see monsterRound and itemRound); a phase is this many rounds.
	// One phase warms the script up, then admitPhases more are
	// measured.
	admitRoundsPerPhase = 250
	admitPhases         = 4
	admitRounds         = (admitPhases + 1) * admitRoundsPerPhase
	// AdmitAllocBudget is how many bytes a script may allocate per
	// round, on average over a phase, whether or not they're freed
	// again. Lua allocates for nearly everything it does -- every
	// number a loop counts through -- so this bounds how much work a
	// hook does as much as how much memory it takes. Far more than a
	// sensible hook needs (a few hundred bytes to a few thousand), far
	// less than a runaway one gets through.
	AdmitAllocBudget = 256 << 10
	// AdmitLeakBudget is how many more bytes a script may still be
	// holding after the measured phases than it was after warming up.
	// A script with bounded state holds no more at all, give or take
	// a few hundred bytes of noise; one that appends to a table every
	// round goes past this within the run.
	AdmitLeakBudget = 16 << 10
	// AdmitTimeout is how long the whole run may take. It happens under
	// the game lock, so this is also the longest a reload can stall the
	// MUD. A typical script takes a few hundredths of a second.
	AdmitTimeout = 2 * time.Second
)

// A variable so tests can run out of time without taking any.
var admitTimeout = AdmitTimeout

// What simulated players say.
var admitPhrases = []string{"hello", "what's the news?", "nice weather", "who are you?", "goodbye"}

// Admit decides whether the script called name, with this source, is
// fit to load: it must compile, return a class, and get through
// simulated traffic without a hook failing, the run taking longer than
// AdmitTimeout, allocating more than AdmitAllocBudget a round, or
// ending up holding AdmitLeakBudget more memory than it started with.
// A nil error means it passed.
//
// definitions are the world's NPC definitions (World.NPCs), so the
// script can room:spawn what it expects to and, if one of them uses
// this script, be tested as that NPC. Nothing in them is changed.
func Admit(ctx context.Context, name, source string, definitions map[string]*world.NPC) error {
	ctx, cancel := context.WithTimeout(ctx, admitTimeout)
	defer cancel()

	run, err := newAdmission(ctx, name, source, definitions)
	if err != nil {
		return err
	}
	defer run.engine.Close()

	// Warm up first: one-off setup (a table a script fills once, Lua's
	// own caches) shouldn't count as growth.
	if err := run.phase(ctx); err != nil {
		return err
	}
	before := heapInUse()
	for range admitPhases {
		if err := run.phase(ctx); err != nil {
			return err
		}
	}
	if after := heapInUse(); after > before+AdmitLeakBudget {
		return fmt.Errorf("%s keeps holding on to more memory the longer it runs (%d bytes more after %d rounds of simulated traffic); "+
			"is it adding to a table forever?", name, after-before, admitPhases*admitRoundsPerPhase)
	}
	return nil
}

// heapInUse is the bytes of heap still in use after a collection.
func heapInUse() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

// allocated is the total bytes ever allocated, freed or not. Cheap
// enough to read every round.
func allocated() uint64 {
	sample := []metrics.Sample{{Name: "/gc/heap/allocs:bytes"}}
	metrics.Read(sample)
	if sample[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return sample[0].Value.Uint64()
}

// admission is one script loaded into a scratch engine, with a small
// world to run it in.
type admission struct {
	name     string
	engine   *Engine
	world    *world.World
	room     *world.Room
	sessions []*session.Session
	players  []*world.Player
	kind     Kind
	// The thing under test: one or the other, by kind.
	npc  *world.NPC
	item *world.Item
	// Something for an item to be swung at.
	target *world.NPC
	round  int
}

const (
	admitStructureID = "admission"
	admitStartID     = "admission-start"
	admitRoomID      = "admission-room"
)

func newAdmission(ctx context.Context, name, source string, definitions map[string]*world.NPC) (*admission, error) {
	w := world.New()
	w.NPCs = definitions
	w.Structures[admitStructureID] = &world.Structure{ID: admitStructureID, Name: "Admission"}
	for _, room := range []*world.Room{
		// First, so it's where a simulated player who dies comes to.
		{ID: admitStartID, StructureID: admitStructureID, Description: "A quiet room.",
			Doorways: []*world.Doorway{{Direction: "north", To: admitRoomID}}},
		{ID: admitRoomID, StructureID: admitStructureID, Description: "A busy room.",
			Doorways: []*world.Doorway{{Direction: "south", To: admitStartID}}},
	} {
		w.AddRoom(room)
		if err := w.PlaceRoom(room); err != nil {
			return nil, err
		}
	}
	room, err := w.GetRoom(admitRoomID)
	if err != nil {
		return nil, err
	}

	sessions := session.NewRegistry()
	engine, err := New(w, sessions, Options{})
	if err != nil {
		return nil, err
	}
	engine.scratch = true
	run := &admission{name: name, engine: engine, world: w, room: room}

	for _, playerName := range []string{"Rincewind", "Twoflower", "Ridcully"} {
		stats := world.DefaultPlayerStats()
		player := &world.Player{Name: playerName, CurrentRoomID: admitRoomID, Stats: &stats}
		if err := w.PlacePlayer(player); err != nil {
			engine.Close()
			return nil, err
		}
		run.players = append(run.players, player)
		run.sessions = append(run.sessions, sessions.Create(player))
	}

	loaded, err := engine.compile(ctx, name, source)
	if err != nil {
		engine.Close()
		return nil, err
	}
	engine.classes[name] = loaded
	run.kind = loaded.kind

	run.target = &world.NPC{ID: "admission-target", Name: "A Practice Dummy", Enemy: true, RoomID: admitRoomID,
		Stats: &world.Stats{Strength: 1, Dexterity: 1, Luck: 1, HitPoints: 100, MaxHitPoints: 100, Stamina: 10, MaxStamina: 10}}
	if loaded.kind == Item {
		run.item = &world.Item{ID: "admission-item", Name: "an item under test", Weapon: &world.Weapon{Damage: 1}, Script: name}
	} else {
		run.npc = admissionNPC(name, definitions)
	}
	return run, nil
}

// admissionNPC is the NPC a monster script is tested as: a spawn of
// the first definition (by id) that uses it, so it has the stats and
// inventory the script was written for, or a stand-in if none does.
func admissionNPC(name string, definitions map[string]*world.NPC) *world.NPC {
	var definition *world.NPC
	for _, candidate := range definitions {
		if candidate.Script == name && (definition == nil || candidate.ID < definition.ID) {
			definition = candidate
		}
	}
	if definition == nil {
		definition = &world.NPC{ID: "admission-npc", Name: "A Monster Under Test"}
	}
	npc := definition.Spawn(admitRoomID)
	npc.Script = name
	if npc.Stats == nil {
		npc.Stats = &world.Stats{Strength: 5, Dexterity: 5, Luck: 5, HitPoints: 20, MaxHitPoints: 20, Stamina: 10, MaxStamina: 10}
	}
	return npc
}

// reset puts the scratch world back the way every round expects it,
// whatever the last one did: the players alive and in the room with
// nothing waiting to be read, and no NPCs but the ones under test.
func (a *admission) reset() error {
	for i, player := range a.players {
		player.Stats.Restore()
		if player.CurrentRoomID != admitRoomID {
			if err := a.world.MovePlayer(player, admitRoomID); err != nil {
				return err
			}
		}
	drain:
		for {
			select {
			case <-a.sessions[i].Messages():
			default:
				break drain
			}
		}
	}
	for _, room := range a.world.Rooms() {
		for _, npc := range room.NPCs {
			if npc != a.npc && npc != a.target {
				a.engine.forget(npc)
			}
		}
		room.NPCs = room.NPCs[:0]
	}
	a.target.RoomID = admitRoomID
	a.target.Stats.Restore()
	a.room.NPCs = append(a.room.NPCs, a.target)
	if a.npc != nil {
		a.npc.RoomID = admitRoomID
		a.npc.Stats.Restore()
		a.room.NPCs = append(a.room.NPCs, a.npc)
	}
	return nil
}

// phase runs admitRoundsPerPhase rounds, failing as soon as a hook
// does, time runs out, or the phase's allocation budget is spent.
func (a *admission) phase(ctx context.Context) error {
	start := allocated()
	budget := uint64(admitRoundsPerPhase * AdmitAllocBudget)
	for range admitRoundsPerPhase {
		if err := a.reset(); err != nil {
			return err
		}
		err := a.runRound(ctx)
		a.round++
		if ctx.Err() != nil {
			// Checked first: once time is up, whichever hook was
			// running fails too, and that isn't the real story.
			return fmt.Errorf("%s is too slow: %d rounds of simulated traffic didn't finish within %v",
				a.name, admitRounds, admitTimeout)
		}
		if err != nil {
			return err
		}
		if used := allocated() - start; used > budget {
			return fmt.Errorf("%s uses too much memory: it allocated %d bytes within %d rounds of simulated traffic (the budget is %d a round)",
				a.name, used, admitRoundsPerPhase, AdmitAllocBudget)
		}
	}
	return nil
}

// check turns the last hook's failure, if there was one, into the
// error Admit reports.
func (a *admission) check(hook string) error {
	if err := a.engine.lastErr; err != nil {
		a.engine.lastErr = nil
		return fmt.Errorf("%s failed under simulated traffic: %w", hook, err)
	}
	return nil
}

func (a *admission) runRound(ctx context.Context) error {
	player := a.players[a.round%len(a.players)]
	if a.kind == Item {
		return a.itemRound(ctx, player)
	}
	return a.monsterRound(ctx, player)
}

// monsterRound is one player walking in, speaking, and attacking, then
// a tick; and every so often the monster dying. The monster's hit
// points step down from round to round, so hooks that behave
// differently when it's hurt get exercised too.
func (a *admission) monsterRound(ctx context.Context, player *world.Player) error {
	e, npc := a.engine, a.npc

	e.PlayerEntered(ctx, player)
	if err := a.check("on_enter"); err != nil {
		return err
	}
	e.PlayerSaid(ctx, player, admitPhrases[a.round%len(admitPhrases)])
	if err := a.check("on_say"); err != nil {
		return err
	}

	npc.Stats.HitPoints = max(1, npc.Stats.MaxHitPoints*(3-a.round%4)/3)
	e.NPCAttacked(ctx, npc, player, func() {
		if !player.Stats.Dead() {
			player.Stats.HitPoints--
		}
	})
	if err := a.check("on_attacked"); err != nil {
		return err
	}

	e.Tick(ctx)
	if err := a.check("on_tick"); err != nil {
		return err
	}

	if a.round%25 == 24 {
		npc.Stats.HitPoints = 0
		// Not NPCDied, which would also drop the state the next round
		// needs.
		e.hook(ctx, npc, npc.Script, "on_death", player, player)
		if err := a.check("on_death"); err != nil {
			return err
		}
	}
	return nil
}

// itemRound is one player picking the item up, landing two blows with
// it, and dropping it again.
func (a *admission) itemRound(ctx context.Context, player *world.Player) error {
	e, item := a.engine, a.item

	player.Inventory = append(player.Inventory[:0], item)
	e.ItemPickedUp(ctx, item, player)
	if err := a.check("on_pickup"); err != nil {
		return err
	}
	for range 2 {
		e.ItemHit(ctx, item, player, a.target, player)
		if err := a.check("on_hit"); err != nil {
			return err
		}
	}
	player.Inventory = player.Inventory[:0]
	a.room.Items = append(a.room.Items[:0], item)
	e.ItemDropped(ctx, item, player)
	a.room.Items = a.room.Items[:0]
	return a.check("on_drop")
}
