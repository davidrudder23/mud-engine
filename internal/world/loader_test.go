package world

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// worldDir is a scratch world directory with structures/ and rooms/
// (npcs/ is optional, created on first writeNPC).
type worldDir string

func newWorldDir(t *testing.T) worldDir {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"structures", "rooms"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return worldDir(dir)
}

func (d worldDir) write(t *testing.T, sub, name string, data any) {
	t.Helper()
	dir := filepath.Join(string(d), sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, ok := data.(string)
	if !ok {
		encoded, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		raw = string(encoded)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

type obj = map[string]any

func (d worldDir) structure(t *testing.T, name string, data any) {
	d.write(t, "structures", name, data)
}
func (d worldDir) room(t *testing.T, name string, data any) { d.write(t, "rooms", name, data) }
func (d worldDir) npc(t *testing.T, name string, data any)  { d.write(t, "npcs", name, data) }

func (d worldDir) mustLoad(t *testing.T) *World {
	t.Helper()
	w, err := Load(string(d))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func mustRoom(t *testing.T, w *World, id string) *Room {
	t.Helper()
	room, err := w.GetRoom(id)
	if err != nil {
		t.Fatal(err)
	}
	return room
}

func TestLoadStructuresAndRooms(t *testing.T) {
	d := newWorldDir(t)
	d.structure(t, "old-mill.json", obj{"id": "old-mill", "name": "Old Mill", "description": "A creaky mill."})
	d.room(t, "cave-entrance.json", obj{
		"id": "cave-entrance", "structure_id": "old-mill", "description": "A damp cave mouth.",
		"items":    []obj{{"id": "torch", "name": "a torch", "description": "Lit."}},
		"doorways": []obj{{"direction": "north", "to": "cave-tunnel", "description": "Narrow."}},
	})
	d.room(t, "cave-tunnel.json", obj{"id": "cave-tunnel", "structure_id": "old-mill"})

	w := d.mustLoad(t)

	if len(w.Structures) != 1 || w.Len() != 2 {
		t.Fatalf("structures=%d rooms=%d", len(w.Structures), w.Len())
	}
	entrance := mustRoom(t, w, "cave-entrance")
	if entrance.Description != "A damp cave mouth." || len(entrance.Items) != 1 || entrance.Items[0].ID != "torch" ||
		entrance.Doorways[0].To != "cave-tunnel" {
		t.Errorf("entrance = %+v", entrance)
	}
	// Rooms are linked into their structure by Load, not left for the
	// caller to wire up.
	structure, _ := w.GetStructure("old-mill")
	if !slices.Equal(structure.Rooms, []*Room{entrance, mustRoom(t, w, "cave-tunnel")}) {
		t.Errorf("structure rooms = %v", structure.Rooms)
	}
}

func TestLoadDefaultsForOptionalFields(t *testing.T) {
	d := newWorldDir(t)
	d.structure(t, "s.json", obj{"id": "s", "name": "S"})
	d.room(t, "r.json", obj{"id": "r", "structure_id": "s"})

	w := d.mustLoad(t)

	structure, _ := w.GetStructure("s")
	room := mustRoom(t, w, "r")
	if structure.Description != "" || room.Description != "" || len(room.Items) != 0 || len(room.Doorways) != 0 ||
		len(room.Players) != 0 || len(room.Commands) != 0 || len(room.NPCs) != 0 {
		t.Errorf("room = %+v", room)
	}
	if len(w.NPCs) != 0 { // missing npcs/ directory is not an error
		t.Errorf("npcs = %v", w.NPCs)
	}
}

func TestLoadRoomCommandsAndItemNameDefault(t *testing.T) {
	d := newWorldDir(t)
	d.structure(t, "s.json", obj{"id": "s", "name": "S"})
	d.room(t, "r.json", obj{"id": "r", "structure_id": "s", "commands": []string{"light torch", "pray"}, "items": []obj{{"id": "rock"}}})

	room := mustRoom(t, d.mustLoad(t), "r")
	if !slices.Equal(room.Commands, []string{"light torch", "pray"}) {
		t.Errorf("commands = %v", room.Commands)
	}
	if room.Items[0].Name != "rock" {
		t.Errorf("item name = %q, want it to default to the id", room.Items[0].Name)
	}
}

func TestLoadOrderDoesNotMatterForForwardReferences(t *testing.T) {
	d := newWorldDir(t)
	d.structure(t, "s.json", obj{"id": "s", "name": "S"})
	d.room(t, "a.json", obj{"id": "room-a", "structure_id": "s", "doorways": []obj{{"direction": "east", "to": "room-b"}}})
	d.room(t, "b.json", obj{"id": "room-b", "structure_id": "s"})

	w := d.mustLoad(t)
	if !w.HasRoom("room-b") || mustRoom(t, w, "room-a").Doorways[0].To != "room-b" {
		t.Error("forward reference not resolved")
	}
	// Rooms() follows file (sorted) order.
	if rooms := w.Rooms(); rooms[0].ID != "room-a" || rooms[1].ID != "room-b" {
		t.Errorf("room order = %v, %v", rooms[0].ID, rooms[1].ID)
	}
}

func TestLoadNPCsAndSpawnsIntoReferencingRooms(t *testing.T) {
	d := newWorldDir(t)
	d.structure(t, "s.json", obj{"id": "s", "name": "S"})
	d.npc(t, "old-tom.json", obj{"id": "old-tom", "name": "Old Tom", "description": "A bartender."})
	d.room(t, "r.json", obj{"id": "r", "structure_id": "s", "npcs": []string{"old-tom"}})

	w := d.mustLoad(t)

	definition, err := w.GetNPC("old-tom")
	if err != nil || definition.Name != "Old Tom" || definition.Description != "A bartender." {
		t.Fatalf("definition = %+v, %v", definition, err)
	}
	room := mustRoom(t, w, "r")
	if len(room.NPCs) != 1 {
		t.Fatalf("room npcs = %v", room.NPCs)
	}
	// A spawned instance, not the definition itself.
	instance := room.NPCs[0]
	if instance == definition || instance.Name != "Old Tom" || instance.RoomID != "r" || definition.RoomID != "" {
		t.Errorf("instance = %+v, definition = %+v", instance, definition)
	}
}

func TestSameNPCInMultipleRoomsAreIndependentInstances(t *testing.T) {
	d := newWorldDir(t)
	d.structure(t, "s.json", obj{"id": "s", "name": "S"})
	d.npc(t, "shopkeeper.json", obj{"id": "shopkeeper", "name": "Shopkeeper", "inventory": []obj{{"id": "rag"}}})
	d.room(t, "a.json", obj{"id": "room-a", "structure_id": "s", "npcs": []string{"shopkeeper"}})
	d.room(t, "b.json", obj{"id": "room-b", "structure_id": "s", "npcs": []string{"shopkeeper"}})

	w := d.mustLoad(t)
	a, b := mustRoom(t, w, "room-a").NPCs[0], mustRoom(t, w, "room-b").NPCs[0]
	if a == b || a.RoomID != "room-a" || b.RoomID != "room-b" {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	// Selling off room-a's shopkeeper's stock doesn't touch room-b's,
	// or the definition's.
	a.Inventory = nil
	if len(b.Inventory) != 1 || len(w.NPCs["shopkeeper"].Inventory) != 1 {
		t.Error("inventory shared between instances")
	}
}

func TestSameNPCTwiceInOneRoom(t *testing.T) {
	d := newWorldDir(t)
	d.structure(t, "s.json", obj{"id": "s", "name": "S"})
	d.npc(t, "guard.json", obj{"id": "guard", "name": "Guard"})
	d.room(t, "r.json", obj{"id": "r", "structure_id": "s", "npcs": []string{"guard", "guard"}})

	npcs := mustRoom(t, d.mustLoad(t), "r").NPCs
	if len(npcs) != 2 || npcs[0] == npcs[1] {
		t.Errorf("npcs = %v", npcs)
	}
}

func TestLoadNPCFields(t *testing.T) {
	d := newWorldDir(t)
	d.structure(t, "s.json", obj{"id": "s", "name": "S"})
	d.npc(t, "defaults.json", obj{"id": "defaults", "name": "Defaults"})
	d.npc(t, "old-tom.json", obj{
		"id": "old-tom", "name": "Old Tom", "hostile": true, "commands": []string{"trade"},
		"inventory": []obj{{"id": "rag", "description": "Damp."}},
		// Only meaningful for "wandering"; silently unused here.
		"move_interval": 45,
	})
	d.room(t, "r.json", obj{"id": "r", "structure_id": "s"})

	w := d.mustLoad(t)

	defaults := w.NPCs["defaults"]
	if defaults.Description != "" || defaults.Hostile || len(defaults.Commands) != 0 || len(defaults.Inventory) != 0 || defaults.IsWandering() {
		t.Errorf("defaults = %+v", defaults)
	}
	tom := w.NPCs["old-tom"]
	if !tom.Hostile || !slices.Equal(tom.Commands, []string{"trade"}) || tom.IsWandering() {
		t.Errorf("old-tom = %+v", tom)
	}
	if len(tom.Inventory) != 1 || tom.Inventory[0].ID != "rag" || tom.Inventory[0].Name != "rag" || tom.Inventory[0].Description != "Damp." {
		t.Errorf("inventory = %+v", tom.Inventory)
	}
}

func TestLoadWanderingNPC(t *testing.T) {
	d := newWorldDir(t)
	d.structure(t, "s.json", obj{"id": "s", "name": "S"})
	d.npc(t, "default.json", obj{"id": "default", "name": "Default", "type": "wandering"})
	d.npc(t, "custom.json", obj{"id": "custom", "name": "Custom", "type": "wandering", "move_interval": 45})
	d.room(t, "r.json", obj{"id": "r", "structure_id": "s", "npcs": []string{"default", "custom"}})

	w := d.mustLoad(t)

	if got := w.NPCs["default"].Wander; got == nil || got.MoveInterval != DefaultMoveInterval {
		t.Errorf("default wander = %+v", got)
	}
	if got := w.NPCs["custom"].Wander; got == nil || got.MoveInterval != 45*time.Second {
		t.Errorf("custom wander = %+v", got)
	}
	instances := mustRoom(t, w, "r").NPCs
	if !instances[0].IsWandering() || instances[1].Wander.MoveInterval != 45*time.Second || instances[0].RoomID != "r" {
		t.Errorf("instances = %+v, %+v", instances[0], instances[1])
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, d worldDir)
	}{
		{"missing structures directory", func(t *testing.T, d worldDir) { os.RemoveAll(filepath.Join(string(d), "structures")) }},
		{"missing rooms directory", func(t *testing.T, d worldDir) { os.RemoveAll(filepath.Join(string(d), "rooms")) }},
		{"malformed json", func(t *testing.T, d worldDir) { d.structure(t, "broken.json", "{not json") }},
		{"structure missing id", func(t *testing.T, d worldDir) { d.structure(t, "s.json", obj{"name": "S"}) }},
		{"structure missing name", func(t *testing.T, d worldDir) { d.structure(t, "s.json", obj{"id": "s"}) }},
		{"room missing id", func(t *testing.T, d worldDir) {
			d.structure(t, "s.json", obj{"id": "s", "name": "S"})
			d.room(t, "r.json", obj{"structure_id": "s"})
		}},
		{"room missing structure_id", func(t *testing.T, d worldDir) {
			d.structure(t, "s.json", obj{"id": "s", "name": "S"})
			d.room(t, "r.json", obj{"id": "r"})
		}},
		{"item missing id", func(t *testing.T, d worldDir) {
			d.structure(t, "s.json", obj{"id": "s", "name": "S"})
			d.room(t, "r.json", obj{"id": "r", "structure_id": "s", "items": []obj{{"name": "a rock"}}})
		}},
		{"duplicate structure id", func(t *testing.T, d worldDir) {
			d.structure(t, "s1.json", obj{"id": "dup", "name": "One"})
			d.structure(t, "s2.json", obj{"id": "dup", "name": "Two"})
		}},
		{"duplicate room id", func(t *testing.T, d worldDir) {
			d.structure(t, "s.json", obj{"id": "s", "name": "S"})
			d.room(t, "r1.json", obj{"id": "dup", "structure_id": "s"})
			d.room(t, "r2.json", obj{"id": "dup", "structure_id": "s"})
		}},
		{"doorway to unknown room", func(t *testing.T, d worldDir) {
			d.structure(t, "s.json", obj{"id": "s", "name": "S"})
			d.room(t, "r.json", obj{"id": "r", "structure_id": "s", "doorways": []obj{{"direction": "north", "to": "nowhere"}}})
		}},
		{"room references unknown structure", func(t *testing.T, d worldDir) {
			d.room(t, "r.json", obj{"id": "r", "structure_id": "ghost-structure"})
		}},
		{"npc missing id", func(t *testing.T, d worldDir) {
			d.structure(t, "s.json", obj{"id": "s", "name": "S"})
			d.npc(t, "old-tom.json", obj{"name": "Old Tom"})
		}},
		{"npc missing name", func(t *testing.T, d worldDir) {
			d.structure(t, "s.json", obj{"id": "s", "name": "S"})
			d.npc(t, "old-tom.json", obj{"id": "old-tom"})
		}},
		{"npc unknown type", func(t *testing.T, d worldDir) {
			d.structure(t, "s.json", obj{"id": "s", "name": "S"})
			d.npc(t, "old-tom.json", obj{"id": "old-tom", "name": "Old Tom", "type": "flying"})
		}},
		{"duplicate npc id", func(t *testing.T, d worldDir) {
			d.structure(t, "s.json", obj{"id": "s", "name": "S"})
			d.npc(t, "a.json", obj{"id": "dup", "name": "One"})
			d.npc(t, "b.json", obj{"id": "dup", "name": "Two"})
		}},
		{"room references unknown npc", func(t *testing.T, d worldDir) {
			d.structure(t, "s.json", obj{"id": "s", "name": "S"})
			d.room(t, "r.json", obj{"id": "r", "structure_id": "s", "npcs": []string{"ghost-npc"}})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newWorldDir(t)
			tt.setup(t, d)
			_, err := Load(string(d))
			var loadErr *LoadError
			if !errors.As(err, &loadErr) {
				t.Errorf("got %v, want a *LoadError", err)
			}
		})
	}
}

// The real authored world must load cleanly.
func TestLoadProjectWorld(t *testing.T) {
	w, err := Load(filepath.Join("..", "..", "world"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Len() == 0 || len(w.Structures) == 0 || len(w.NPCs) == 0 {
		t.Errorf("structures=%d rooms=%d npcs=%d", len(w.Structures), w.Len(), len(w.NPCs))
	}
}
