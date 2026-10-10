package scripting

// Where the game calls into scripts. Each of these is called by the
// command (or loop) where the thing happens, with the game lock held,
// and returns the lines the script addressed to actor -- the player
// whose command it is -- for that command to add to its own reply.
// Everyone else's lines have already been sent by the time it returns.
//
// All of them are safe on a nil *Engine, where they do what a world
// with no scripts would.

import (
	"context"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"mudengine/internal/world"
)

// TickInterval is how often on_tick runs.
const TickInterval = 5 * time.Second

// hook calls script's `name` hook on obj. ran is false if nothing ran
// to completion: no engine, no such script loaded, the hook isn't
// overridden (so there's nothing to add to the default behaviour), or
// it failed. Lines for actor are returned either way.
func (e *Engine) hook(ctx context.Context, obj any, script, name string, actor *world.Player, args ...any) (lines []string, ran bool) {
	if e == nil || e.running {
		// A hook can't trigger another: nothing a script can do through
		// the API calls back into one.
		return nil, false
	}
	loaded := e.active(script)
	if loaded == nil {
		return nil, false
	}
	fn, ok := e.L.GetField(loaded.table, name).(*lua.LFunction)
	if !ok || lua.LValue(fn) == e.L.GetField(e.base(loaded.kind), name) {
		return nil, false
	}

	largs := make([]lua.LValue, 0, len(args)+1)
	largs = append(largs, e.instance(obj, script))
	for _, arg := range args {
		largs = append(largs, e.value(arg))
	}

	e.running, e.ctx, e.actor, e.replies, e.lastErr = true, ctx, actor, nil, nil
	_, err := e.pcall(ctx, fn, 0, largs...)
	lines = e.replies
	e.running, e.ctx, e.actor, e.replies = false, nil, nil, nil

	if err != nil {
		e.lastErr = err
		if !e.scratch {
			loaded.failures++
			logger.Warn("Script hook failed", "script", script, "hook", name, "error", err, "failures", loaded.failures)
			if loaded.failures >= MaxFailures {
				loaded.disabled = true
				logger.Error("Script disabled after repeated failures; reload it to re-enable",
					"script", script, "failures", loaded.failures)
			}
		}
		return lines, false
	}
	loaded.failures = 0
	return lines, true
}

// scriptedNPCs is the NPCs in room with a script, as a copy: a hook can
// spawn into the room while they're being walked.
func scriptedNPCs(room *world.Room) []*world.NPC {
	var npcs []*world.NPC
	for _, npc := range room.NPCs {
		if npc.Script != "" {
			npcs = append(npcs, npc)
		}
	}
	return npcs
}

// roomHook calls one hook on every scripted NPC in player's room, for
// as long as the player is still in it.
func (e *Engine) roomHook(ctx context.Context, player *world.Player, name string, args ...any) []string {
	if e == nil || len(e.classes) == 0 {
		return nil
	}
	room := e.room(player.CurrentRoomID)
	if room == nil {
		return nil
	}
	var lines []string
	for _, npc := range scriptedNPCs(room) {
		if player.CurrentRoomID != room.ID {
			break // a hook moved them (killed them, say)
		}
		if npc.RoomID != room.ID {
			continue
		}
		more, _ := e.hook(ctx, npc, npc.Script, name, player, args...)
		lines = append(lines, more...)
	}
	return lines
}

// PlayerEntered runs on_enter(player) on the NPCs in the room player
// just walked into.
func (e *Engine) PlayerEntered(ctx context.Context, player *world.Player) []string {
	return e.roomHook(ctx, player, "on_enter", player)
}

// PlayerSaid runs on_say(player, text) on the NPCs in the room player
// just spoke in.
func (e *Engine) PlayerSaid(ctx context.Context, player *world.Player, text string) []string {
	return e.roomHook(ctx, player, "on_say", player, text)
}

// NPCAttacked runs on_attacked(attacker) on npc, which attacker has
// just attacked without killing. strike is npc's attack back. It's
// what the base class's on_attacked does, so a script decides whether
// and when it happens by calling that or not; with no script, or one
// that fails before getting that far, it happens here. Either way
// it's called at most once.
func (e *Engine) NPCAttacked(ctx context.Context, npc *world.NPC, attacker *world.Player, strike func()) []string {
	struck := false
	once := func() {
		if !struck {
			struck = true
			strike()
		}
	}
	if e == nil {
		once()
		return nil
	}
	e.strike = once
	lines, ran := e.hook(ctx, npc, npc.Script, "on_attacked", attacker, attacker)
	e.strike = nil
	if !ran {
		once()
	}
	return lines
}

// NPCDied runs on_death(killer) on npc. Call it once npc is dead but
// before World.KillNPC removes it, so the script can still see the
// room it died in. npc's instance state is dropped afterwards.
func (e *Engine) NPCDied(ctx context.Context, npc *world.NPC, killer *world.Player) []string {
	if e == nil {
		return nil
	}
	lines, _ := e.hook(ctx, npc, npc.Script, "on_death", killer, killer)
	e.forget(npc)
	return lines
}

// ItemPickedUp runs on_pickup(player) on item, now in player's
// inventory.
func (e *Engine) ItemPickedUp(ctx context.Context, item *world.Item, player *world.Player) []string {
	lines, _ := e.hook(ctx, item, item.Script, "on_pickup", player, player)
	return lines
}

// ItemDropped runs on_drop(player) on item, now lying in player's room.
func (e *Engine) ItemDropped(ctx context.Context, item *world.Item, player *world.Player) []string {
	lines, _ := e.hook(ctx, item, item.Script, "on_drop", player, player)
	return lines
}

// ItemHit runs on_hit(wielder, target) on item, the weapon wielder
// (a *world.Player or *world.NPC) has just hit target (the other) with.
// actor is the player whose command this fight is.
func (e *Engine) ItemHit(ctx context.Context, item *world.Item, wielder, target any, actor *world.Player) []string {
	lines, _ := e.hook(ctx, item, item.Script, "on_hit", actor, wielder, target)
	return lines
}

// Tick runs on_tick() on every scripted NPC in the world.
func (e *Engine) Tick(ctx context.Context) {
	if e == nil || len(e.classes) == 0 {
		return
	}
	var npcs []*world.NPC
	for _, room := range e.world.Rooms() {
		npcs = append(npcs, scriptedNPCs(room)...)
	}
	for _, npc := range npcs {
		if npc.RoomID != "" {
			e.hook(ctx, npc, npc.Script, "on_tick", nil)
		}
	}
}

// wait blocks for d or until ctx is done -- a variable so tests can run
// the loop without real time passing.
var wait = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Run calls Tick every TickInterval, holding lock (the game lock),
// until ctx is cancelled; then returns ctx's error.
func (e *Engine) Run(ctx context.Context, lock sync.Locker) error {
	for {
		if err := wait(ctx, TickInterval); err != nil {
			return err
		}
		lock.Lock()
		e.Tick(ctx)
		lock.Unlock()
	}
}
