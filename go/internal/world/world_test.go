package world_test

import (
	"errors"
	"slices"
	"testing"

	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

func roomIDs(w *world.World) []string {
	var ids []string
	for _, r := range w.Rooms() {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestLookupMissingIsNotFound(t *testing.T) {
	w := world.New()
	if _, err := w.GetRoom("nowhere"); !errors.Is(err, world.ErrNotFound) {
		t.Errorf("GetRoom: got %v, want ErrNotFound", err)
	}
	if _, err := w.GetStructure("nowhere"); !errors.Is(err, world.ErrNotFound) {
		t.Errorf("GetStructure: got %v, want ErrNotFound", err)
	}
	if _, err := w.GetNPC("nowhere"); !errors.Is(err, world.ErrNotFound) {
		t.Errorf("GetNPC: got %v, want ErrNotFound", err)
	}
}

func TestLenAndRoomsReflectRooms(t *testing.T) {
	w := worldtest.TwoRooms()
	if w.Len() != 2 {
		t.Errorf("Len() = %d, want 2", w.Len())
	}
	if got := roomIDs(w); !slices.Equal(got, []string{worldtest.StartID, worldtest.NorthID}) {
		t.Errorf("Rooms() ids = %v", got)
	}
}

func TestPlaceRoomIsIdempotent(t *testing.T) {
	w := worldtest.TwoRooms()
	structure, _ := w.GetStructure(worldtest.StructureID)
	before := slices.Clone(structure.Rooms)

	if err := w.PlaceRoom(worldtest.MustRoom(w, worldtest.StartID)); err != nil { // already placed
		t.Fatal(err)
	}
	if !slices.Equal(structure.Rooms, before) {
		t.Errorf("rooms changed: %v -> %v", before, structure.Rooms)
	}
}

func TestMoveRoomToStructure(t *testing.T) {
	w := worldtest.TwoRooms()
	other := &world.Structure{ID: "other-structure", Name: "Other"}
	w.Structures[other.ID] = other
	room := worldtest.MustRoom(w, worldtest.StartID)
	original, _ := w.GetStructure(worldtest.StructureID)

	if err := w.MoveRoomToStructure(room, other.ID); err != nil {
		t.Fatal(err)
	}
	if room.StructureID != other.ID {
		t.Errorf("StructureID = %q", room.StructureID)
	}
	if !slices.Contains(other.Rooms, room) || slices.Contains(original.Rooms, room) {
		t.Error("room not moved between structure lists")
	}
}

func TestMoveRoomToUnknownStructureDoesNotMutate(t *testing.T) {
	w := worldtest.TwoRooms()
	room := worldtest.MustRoom(w, worldtest.StartID)
	original, _ := w.GetStructure(worldtest.StructureID)

	if err := w.MoveRoomToStructure(room, "ghost-structure"); !errors.Is(err, world.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if room.StructureID != worldtest.StructureID || !slices.Contains(original.Rooms, room) {
		t.Error("room was mutated despite the error")
	}
}

func TestPlacePlayer(t *testing.T) {
	w := worldtest.TwoRooms()
	player := &world.Player{Name: "alice", CurrentRoomID: worldtest.StartID}

	if err := w.PlacePlayer(player); err != nil {
		t.Fatal(err)
	}
	if err := w.PlacePlayer(player); err != nil { // idempotent
		t.Fatal(err)
	}
	players := worldtest.MustRoom(w, worldtest.StartID).Players
	if len(players) != 1 || players[0] != player {
		t.Errorf("players = %v, want just alice once", players)
	}
}

func TestPlacePlayerUnknownRoom(t *testing.T) {
	w := worldtest.TwoRooms()
	if err := w.PlacePlayer(&world.Player{Name: "alice", CurrentRoomID: "nowhere"}); !errors.Is(err, world.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestTwoPlayersSameNameAreIndependent(t *testing.T) {
	// Two logins under the same name are two distinct *Player values,
	// compared by identity, not by field values.
	w := worldtest.TwoRooms()
	alice1 := &world.Player{Name: "alice", CurrentRoomID: worldtest.StartID}
	alice2 := &world.Player{Name: "alice", CurrentRoomID: worldtest.StartID}
	w.PlacePlayer(alice1)
	w.PlacePlayer(alice2)

	players := worldtest.MustRoom(w, worldtest.StartID).Players
	if len(players) != 2 || !slices.Contains(players, alice1) || !slices.Contains(players, alice2) {
		t.Errorf("players = %v, want both alices", players)
	}
}

func TestMovePlayer(t *testing.T) {
	w := worldtest.TwoRooms()
	player := &world.Player{Name: "alice", CurrentRoomID: worldtest.StartID}
	w.PlacePlayer(player)

	if err := w.MovePlayer(player, worldtest.NorthID); err != nil {
		t.Fatal(err)
	}
	if player.CurrentRoomID != worldtest.NorthID {
		t.Errorf("CurrentRoomID = %q", player.CurrentRoomID)
	}
	if slices.Contains(worldtest.MustRoom(w, worldtest.StartID).Players, player) ||
		!slices.Contains(worldtest.MustRoom(w, worldtest.NorthID).Players, player) {
		t.Error("occupant lists not updated")
	}
}

func TestMovePlayerUnknownDestinationDoesNotMutate(t *testing.T) {
	w := worldtest.TwoRooms()
	player := &world.Player{Name: "alice", CurrentRoomID: worldtest.StartID}
	w.PlacePlayer(player)

	if err := w.MovePlayer(player, "nowhere"); !errors.Is(err, world.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if player.CurrentRoomID != worldtest.StartID || !slices.Contains(worldtest.MustRoom(w, worldtest.StartID).Players, player) {
		t.Error("player was mutated despite the error")
	}
}

func TestMovePlayerNotPreviouslyPlaced(t *testing.T) {
	w := worldtest.TwoRooms()
	player := &world.Player{Name: "alice", CurrentRoomID: worldtest.StartID} // never placed

	if err := w.MovePlayer(player, worldtest.NorthID); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(worldtest.MustRoom(w, worldtest.NorthID).Players, player) {
		t.Error("player not in new room")
	}
}

func TestMoveNPC(t *testing.T) {
	w := worldtest.TwoRooms()
	start := worldtest.MustRoom(w, worldtest.StartID)
	npc := &world.NPC{ID: "old-tom", Name: "Old Tom", RoomID: worldtest.StartID}
	start.NPCs = append(start.NPCs, npc)

	if err := w.MoveNPC(npc, worldtest.NorthID); err != nil {
		t.Fatal(err)
	}
	if npc.RoomID != worldtest.NorthID || slices.Contains(start.NPCs, npc) ||
		!slices.Contains(worldtest.MustRoom(w, worldtest.NorthID).NPCs, npc) {
		t.Error("npc not moved")
	}
}

func TestMoveNPCUnknownDestinationDoesNotMutate(t *testing.T) {
	w := worldtest.TwoRooms()
	start := worldtest.MustRoom(w, worldtest.StartID)
	npc := &world.NPC{ID: "old-tom", Name: "Old Tom", RoomID: worldtest.StartID}
	start.NPCs = append(start.NPCs, npc)

	if err := w.MoveNPC(npc, "nowhere"); !errors.Is(err, world.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if npc.RoomID != worldtest.StartID || !slices.Contains(start.NPCs, npc) {
		t.Error("npc was mutated despite the error")
	}
}

func TestMoveNPCNotPreviouslyPlaced(t *testing.T) {
	w := worldtest.TwoRooms()
	npc := &world.NPC{ID: "old-tom", Name: "Old Tom"} // a definition, never placed

	if err := w.MoveNPC(npc, worldtest.NorthID); err != nil {
		t.Fatal(err)
	}
	if npc.RoomID != worldtest.NorthID || !slices.Contains(worldtest.MustRoom(w, worldtest.NorthID).NPCs, npc) {
		t.Error("npc not placed in new room")
	}
}

func TestRemovePlayerKeepsCurrentRoomID(t *testing.T) {
	w := worldtest.TwoRooms()
	player := &world.Player{Name: "alice", CurrentRoomID: worldtest.StartID}
	w.PlacePlayer(player)

	w.RemovePlayer(player)

	if slices.Contains(worldtest.MustRoom(w, worldtest.StartID).Players, player) {
		t.Error("player still in room")
	}
	if player.CurrentRoomID != worldtest.StartID {
		t.Errorf("CurrentRoomID changed to %q", player.CurrentRoomID)
	}

	w.RemovePlayer(&world.Player{Name: "bob", CurrentRoomID: worldtest.StartID}) // never placed: no-op
}
