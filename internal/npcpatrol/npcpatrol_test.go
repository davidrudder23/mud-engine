package npcpatrol

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

func wanderer(roomID string) *world.NPC {
	return &world.NPC{ID: "watchman", Name: "A Watchman", RoomID: roomID, Wander: &world.Wander{}}
}

func TestWanderOnceMovesThroughADoorway(t *testing.T) {
	// "start" has exactly one doorway (north), so this is deterministic.
	w := worldtest.TwoRooms()
	start := worldtest.MustRoom(w, worldtest.StartID)
	npc := wanderer(worldtest.StartID)
	start.NPCs = append(start.NPCs, npc)

	if err := WanderOnce(w, npc, nil); err != nil {
		t.Fatal(err)
	}
	if npc.RoomID != worldtest.NorthID || slices.Contains(start.NPCs, npc) ||
		!slices.Contains(worldtest.MustRoom(w, worldtest.NorthID).NPCs, npc) {
		t.Error("npc didn't move north")
	}
}

func TestWanderOnceWithNoDoorwaysIsANoOp(t *testing.T) {
	w := world.New()
	w.Structures["s"] = &world.Structure{ID: "s", Name: "S"}
	room := &world.Room{ID: "dead-end", StructureID: "s"}
	w.AddRoom(room)
	npc := wanderer(room.ID)
	room.NPCs = append(room.NPCs, npc)

	if err := WanderOnce(w, npc, nil); err != nil {
		t.Fatal(err)
	}
	if npc.RoomID != room.ID || !slices.Contains(room.NPCs, npc) {
		t.Error("npc moved out of a room with no doorways")
	}
}

func TestWanderOnceUsesPickToChooseADoorway(t *testing.T) {
	w := world.New()
	w.Structures["s"] = &world.Structure{ID: "s", Name: "S"}
	hub := &world.Room{ID: "hub", StructureID: "s"}
	for _, d := range []struct{ dir, to string }{{"north", "a"}, {"east", "b"}, {"south", "c"}} {
		hub.Doorways = append(hub.Doorways, &world.Doorway{Direction: d.dir, To: d.to})
		w.AddRoom(&world.Room{ID: d.to, StructureID: "s"})
	}
	w.AddRoom(hub)
	npc := wanderer("hub")
	hub.NPCs = append(hub.NPCs, npc)

	if err := WanderOnce(w, npc, func(int) int { return 1 }); err != nil { // always "east" -> "b"
		t.Fatal(err)
	}
	if npc.RoomID != "b" {
		t.Errorf("RoomID = %q, want b", npc.RoomID)
	}
}

func TestRunForeverMovesRepeatedlyUntilCancelled(t *testing.T) {
	w := worldtest.TwoRooms()
	npc := wanderer(worldtest.StartID)
	start := worldtest.MustRoom(w, worldtest.StartID)
	start.NPCs = append(start.NPCs, npc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := 0
	original := wait
	t.Cleanup(func() { wait = original })
	wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits > 5 {
			cancel() // stop the loop deterministically
			return ctx.Err()
		}
		return nil
	}

	err := RunForever(ctx, w, npc, &sync.Mutex{})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
	// Five moves between the two rooms: start -> north -> ... -> north.
	if waits != 6 || npc.RoomID != worldtest.NorthID {
		t.Errorf("waits=%d room=%q", waits, npc.RoomID)
	}
}

func TestRunForeverStopsOnCancellation(t *testing.T) {
	w := worldtest.TwoRooms()
	npc := wanderer(worldtest.StartID)
	npc.Wander.MoveInterval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error)
	go func() { done <- RunForever(ctx, w, npc, &sync.Mutex{}) }()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunForever didn't stop after cancel")
	}
}
