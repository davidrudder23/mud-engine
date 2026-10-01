package world

// Structure is a building, cavern, or other container that groups rooms
// together (e.g. "Old Mill", "Sunken Cave"). Every Room belongs to
// exactly one Structure; a Structure can contain many Rooms.
type Structure struct {
	ID          string
	Name        string
	Description string
	// Kept in sync with each room's StructureID via World.PlaceRoom /
	// World.MoveRoomToStructure -- nothing else should touch this
	// slice directly.
	Rooms []*Room
}

func (s *Structure) Format() string {
	return s.Name + "\r\n" + s.Description
}
