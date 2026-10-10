package commands

import (
	"context"
	"strings"
	"testing"

	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

func TestTeleport(t *testing.T) {
	f := newFixture(t)
	// A third room no doorway leads to, whose id contains another's.
	far := &world.Room{ID: "far-north-room", StructureID: worldtest.StructureID, Description: "Somewhere remote."}
	f.world.AddRoom(far)
	if err := f.world.PlaceRoom(far); err != nil {
		t.Fatal(err)
	}
	teleport := NewTeleportCommand(f.sessions, f.world, f.repo)

	tests := []struct {
		line string
		// Where the player ends up, and what the reply starts with.
		wantRoom, wantReply string
	}{
		{"teleport", worldtest.StartID, "Teleport where?"},
		{"teleport nowhere", worldtest.StartID, "There's no room called 'nowhere'."},
		{"teleport start", worldtest.StartID, "You're already there."},
		// Two ids contain "north"; nobody moves.
		{"teleport north", worldtest.StartID, "'north' could be any of: north-room, far-north-room."},
		// An exact id wins even though another id contains it.
		{"teleport north-room", worldtest.NorthID, "[north-room]"},
		{"teleport FAR", "far-north-room", "[far-north-room]\r\nSomewhere remote."},
		{"teleport  Start ", worldtest.StartID, "[start]"},
	}
	for _, tt := range tests {
		got := f.run(t, teleport, tt.line)
		if !strings.HasPrefix(got, tt.wantReply) {
			t.Errorf("%q = %q, want it to start %q", tt.line, got, tt.wantReply)
		}
		if f.player.CurrentRoomID != tt.wantRoom {
			t.Errorf("%q left the player in %q, want %q", tt.line, f.player.CurrentRoomID, tt.wantRoom)
		}
	}
}

func TestTeleportMovesOccupancyAndSaves(t *testing.T) {
	f := newFixture(t)
	teleport := NewTeleportCommand(f.sessions, f.world, f.repo)

	f.run(t, teleport, "teleport north-room")

	if len(f.room(worldtest.StartID).Players) != 0 || len(f.room(worldtest.NorthID).Players) != 1 {
		t.Error("room occupancy wasn't updated")
	}
	saved, err := f.repo.GetByName(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if saved.CurrentRoomID != worldtest.NorthID {
		t.Errorf("saved room = %q", saved.CurrentRoomID)
	}
}

func TestTeleportRunsTheRoomsScripts(t *testing.T) {
	f := newFixture(t)
	engine, commands := f.scripted(t)
	f.script(t, engine, "rat.lua", `
		local Rat = Monster:extend()
		function Rat:on_enter(player) self:emote("looks startled.") end
		return Rat`)
	rat := f.addTroll(worldtest.NorthID)
	rat.Name, rat.Script = "A Rat", "rat.lua"

	got := lines(f.run(t, commands["teleport"], "teleport north-room"))
	if got[len(got)-1] != "A Rat looks startled." {
		t.Errorf("teleport = %q", got)
	}
}
