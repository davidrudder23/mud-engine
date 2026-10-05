package world

import (
	"slices"
	"testing"
	"time"
)

func TestNPCDefaults(t *testing.T) {
	npc := &NPC{ID: "old-tom", Name: "Old Tom"}
	if npc.Description != "" || npc.Enemy || npc.Stats != nil || len(npc.Commands) != 0 || len(npc.Inventory) != 0 ||
		npc.RoomID != "" || npc.IsWandering() {
		t.Errorf("unexpected defaults: %+v", npc)
	}
	if npc.Format() != "Old Tom" {
		t.Errorf("Format() = %q", npc.Format())
	}
}

func TestNPCCommands(t *testing.T) {
	tests := []struct {
		name string
		npc  NPC
		want []string
	}{
		{"always offers talk", NPC{}, []string{"talk"}},
		{"authored commands appended in order", NPC{Commands: []string{"trade", "follow"}}, []string{"talk", "trade", "follow"}},
		{"dedupes", NPC{Commands: []string{"talk", "trade", "talk"}}, []string{"talk", "trade"}},
		// Deliberate for now: an inventory item's own commands aren't
		// offered just because an NPC is carrying it.
		{"ignores inventory", NPC{Inventory: []*Item{{ID: "sword"}}}, []string{"talk"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.npc.GetCommands(); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSpawnCopiesStaticFieldsAndSetsRoomID(t *testing.T) {
	definition := &NPC{ID: "old-tom", Name: "Old Tom", Description: "A bartender.", Enemy: true, Commands: []string{"trade"}}
	instance := definition.Spawn("taproom")

	if instance == definition {
		t.Fatal("Spawn returned the definition itself")
	}
	if instance.ID != "old-tom" || instance.Name != "Old Tom" || instance.Description != "A bartender." ||
		!instance.Enemy || !slices.Equal(instance.Commands, []string{"trade"}) {
		t.Errorf("fields not copied: %+v", instance)
	}
	if instance.RoomID != "taproom" {
		t.Errorf("RoomID = %q", instance.RoomID)
	}
	if definition.RoomID != "" {
		t.Error("definition was given a room")
	}
}

func TestTwoSpawnsAreIndependent(t *testing.T) {
	definition := &NPC{ID: "shopkeeper", Name: "Shopkeeper"}
	a := definition.Spawn("room-a")
	b := definition.Spawn("room-b")
	if a == b || a.RoomID != "room-a" || b.RoomID != "room-b" {
		t.Fatalf("a=%+v b=%+v", a, b)
	}

	a.Commands = append(a.Commands, "haggle")
	a.Inventory = append(a.Inventory, &Item{ID: "rag"})
	if len(b.Commands) != 0 || len(b.Inventory) != 0 {
		t.Errorf("mutating a leaked into b: %+v", b)
	}
}

func TestSpawnCopiesInventoryItemsIndependently(t *testing.T) {
	definition := &NPC{ID: "old-tom", Name: "Old Tom", Inventory: []*Item{{ID: "rag", Name: "a rag"}}}
	instance := definition.Spawn("taproom")

	if len(instance.Inventory) != 1 || instance.Inventory[0] == definition.Inventory[0] || instance.Inventory[0].Name != "a rag" {
		t.Fatalf("inventory = %+v", instance.Inventory)
	}
	instance.Inventory = instance.Inventory[:0]
	if len(definition.Inventory) != 1 {
		t.Error("clearing the instance's inventory touched the definition")
	}
}

func TestSpawningASpawnWorks(t *testing.T) {
	definition := &NPC{ID: "old-tom", Name: "Old Tom", Commands: []string{"trade"}}
	instance := definition.Spawn("room-a")
	instance.Inventory = append(instance.Inventory, &Item{ID: "rag"})

	respawn := instance.Spawn("room-b")

	if respawn == instance || respawn.RoomID != "room-b" || instance.RoomID != "room-a" {
		t.Errorf("respawn=%+v instance=%+v", respawn, instance)
	}
	if !slices.Equal(respawn.Commands, []string{"trade"}) || len(respawn.Inventory) != 1 || respawn.Inventory[0] == instance.Inventory[0] {
		t.Errorf("respawn = %+v", respawn)
	}
}

func TestWanderingNPC(t *testing.T) {
	definition := &NPC{ID: "watchman", Name: "A Watchman", Wander: &Wander{MoveInterval: 45 * time.Second}}
	if !definition.IsWandering() || !slices.Equal(definition.GetCommands(), []string{"talk"}) {
		t.Errorf("definition = %+v", definition)
	}

	instance := definition.Spawn("muster-room")
	if !instance.IsWandering() || instance.Wander.MoveInterval != 45*time.Second || instance.RoomID != "muster-room" {
		t.Errorf("instance = %+v", instance)
	}
	if instance.Wander == definition.Wander {
		t.Error("spawn shares the definition's Wander")
	}
	if respawn := instance.Spawn("room-b"); respawn.Wander.MoveInterval != 45*time.Second {
		t.Errorf("respawn interval = %v", respawn.Wander.MoveInterval)
	}
}
