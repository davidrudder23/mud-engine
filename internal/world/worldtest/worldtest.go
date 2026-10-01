// Package worldtest builds small, known-shape Worlds for tests that
// don't care about JSON loading itself.
package worldtest

import "mudengine/internal/world"

const (
	StructureID = "test-structure"
	StartID     = "start"
	NorthID     = "north-room"
)

// TwoRooms is a two-room world, StartID <-> NorthID, connected both
// ways ("north" from start, "south" back).
func TwoRooms() *world.World {
	w := world.New()
	w.Structures[StructureID] = &world.Structure{ID: StructureID, Name: "Test Structure"}

	rooms := []*world.Room{
		{
			ID:          StartID,
			StructureID: StructureID,
			Description: "The starting room.",
			Doorways:    []*world.Doorway{{Direction: "north", To: NorthID}},
		},
		{
			ID:          NorthID,
			StructureID: StructureID,
			Description: "A room to the north.",
			Doorways:    []*world.Doorway{{Direction: "south", To: StartID}},
		},
	}
	for _, room := range rooms {
		w.AddRoom(room)
		if err := w.PlaceRoom(room); err != nil {
			panic(err)
		}
	}
	return w
}

// MustRoom is GetRoom for tests: it panics on an unknown id.
func MustRoom(w *world.World, id string) *world.Room {
	room, err := w.GetRoom(id)
	if err != nil {
		panic(err)
	}
	return room
}
