// Package npcpatrol runs wandering NPCs: every Wander.MoveInterval, one
// moves through a random doorway of whatever room it's standing in.
//
// WanderOnce is the actual logic (synchronous, easy to test without
// waiting on real time); RunForever is the long-lived per-NPC loop
// around it that the telnet server starts one of, as a goroutine, for
// every wandering NPC the loaded world has.
//
// Deliberately one goroutine per wandering NPC rather than one shared
// ticker: each NPC can have its own interval, a slow move for one can't
// delay another, and stopping one doesn't touch the rest.
package npcpatrol

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"mudengine/internal/logging"
	"mudengine/internal/world"
)

var logger = logging.For("mud.npcpatrol")

// WanderOnce moves npc through one random doorway of its current room.
// A room with no doorways is a no-op -- there's nowhere to wander to.
//
// pick chooses the doorway: given n, it returns an index in [0, n).
// Nil means math/rand/v2's IntN; tests pass a fixed choice.
func WanderOnce(w *world.World, npc *world.NPC, pick func(n int) int) error {
	room, err := w.GetRoom(npc.RoomID)
	if err != nil {
		return err
	}
	if len(room.Doorways) == 0 {
		return nil
	}
	if pick == nil {
		pick = rand.IntN
	}

	doorway := room.Doorways[pick(len(room.Doorways))]
	if err := w.MoveNPC(npc, doorway.To); err != nil {
		return err
	}
	logger.Info("NPC wandered", "npc", npc.Name, "from", room.ID, "to", doorway.To)
	return nil
}

// wait blocks for d or until ctx is done, returning ctx's error in the
// latter case. A variable so tests can run the loop without real time
// passing.
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

// RunForever moves npc once every npc.Wander.MoveInterval until ctx is
// cancelled (returning ctx's error) or npc leaves the world -- killed,
// via World.KillNPC, which clears its RoomID (returning nil). Each move
// happens while holding lock -- the game lock everything else touching
// the world holds too. A failed move is logged and the loop carries on.
func RunForever(ctx context.Context, w *world.World, npc *world.NPC, lock sync.Locker) error {
	for {
		if err := wait(ctx, npc.Wander.MoveInterval); err != nil {
			return err
		}
		lock.Lock()
		if npc.RoomID == "" {
			lock.Unlock()
			logger.Info("NPC patrol ended", "npc", npc.Name)
			return nil
		}
		err := WanderOnce(w, npc, nil)
		lock.Unlock()
		if err != nil {
			logger.Warn("NPC wander failed", "npc", npc.Name, "error", err)
		}
	}
}
