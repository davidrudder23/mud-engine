package world

import (
	"errors"
	"fmt"
)

// ErrNotFound is wrapped by every "no such room/structure/npc" error.
var ErrNotFound = errors.New("not found")

// World is the full set of loaded structures/rooms, plus player
// placement/movement and room/structure membership.
//
// World does no locking of its own. The telnet server, MCP server, and
// NPC patrols share one game lock around anything that touches it (see
// the telnet package).
type World struct {
	Structures map[string]*Structure
	// NPC *definitions* (templates), one per npcs/*.json id -- not the
	// NPCs actually standing in rooms. Those are separate Spawn
	// instances living in each Room.NPCs.
	NPCs map[string]*NPC
	// Where the Lua files that NPC.Script / Item.Script name live
	// (<world_dir>/scripts for a loaded world); empty for a world
	// with no scripts.
	ScriptsDir string

	rooms map[string]*Room
	// Load order of rooms: Rooms() iterates in it, and the first one
	// is the default starting room.
	roomOrder []string
}

func New() *World {
	return &World{
		Structures: map[string]*Structure{},
		NPCs:       map[string]*NPC{},
		rooms:      map[string]*Room{},
	}
}

// AddRoom registers room with the world, without placing it into its
// structure (see PlaceRoom). A room id that's already registered is
// replaced in place, keeping its original position in Rooms().
func (w *World) AddRoom(room *Room) {
	if _, exists := w.rooms[room.ID]; !exists {
		w.roomOrder = append(w.roomOrder, room.ID)
	}
	w.rooms[room.ID] = room
}

// HasRoom reports whether a room with this id is registered.
func (w *World) HasRoom(roomID string) bool {
	_, ok := w.rooms[roomID]
	return ok
}

// Rooms returns every room, in the order they were added.
func (w *World) Rooms() []*Room {
	rooms := make([]*Room, 0, len(w.roomOrder))
	for _, id := range w.roomOrder {
		rooms = append(rooms, w.rooms[id])
	}
	return rooms
}

// StartingRoomID is where new players appear, and where a player who
// dies comes back to. Arbitrary but deterministic: the first loaded
// room. There's no "spawn point" concept yet -- if the world ever needs
// one, mark it in JSON rather than relying on load order.
func (w *World) StartingRoomID() string {
	return w.roomOrder[0]
}

// Len is the number of rooms.
func (w *World) Len() int {
	return len(w.rooms)
}

func (w *World) GetRoom(roomID string) (*Room, error) {
	room, ok := w.rooms[roomID]
	if !ok {
		return nil, fmt.Errorf("no such room: %q: %w", roomID, ErrNotFound)
	}
	return room, nil
}

func (w *World) GetStructure(structureID string) (*Structure, error) {
	structure, ok := w.Structures[structureID]
	if !ok {
		return nil, fmt.Errorf("no such structure: %q: %w", structureID, ErrNotFound)
	}
	return structure, nil
}

// GetNPC returns the NPC *definition* for npcID -- not a specific
// room's spawned instance. Look up a live NPC standing in a room via
// that room's own NPCs instead.
func (w *World) GetNPC(npcID string) (*NPC, error) {
	npc, ok := w.NPCs[npcID]
	if !ok {
		return nil, fmt.Errorf("no such npc: %q: %w", npcID, ErrNotFound)
	}
	return npc, nil
}

// PlaceRoom adds a room to its structure's room list.
//
// Use this once per room when the world loads (room.StructureID is
// already set, e.g. from JSON) -- after that, use MoveRoomToStructure
// for any change of structure. Safe to call again for a room that's
// already placed (it's a no-op).
func (w *World) PlaceRoom(room *Room) error {
	structure, err := w.GetStructure(room.StructureID)
	if err != nil {
		return err
	}
	if !containsPtr(structure.Rooms, room) {
		structure.Rooms = append(structure.Rooms, room)
	}
	return nil
}

// MoveRoomToStructure moves a room to a different structure, updating
// room.StructureID and both structures' room lists together so they
// can never drift out of sync.
func (w *World) MoveRoomToStructure(room *Room, toStructureID string) error {
	newStructure, err := w.GetStructure(toStructureID) // validate first
	if err != nil {
		return err
	}
	if oldStructure, ok := w.Structures[room.StructureID]; ok {
		oldStructure.Rooms, _ = removePtr(oldStructure.Rooms, room)
	}
	newStructure.Rooms = append(newStructure.Rooms, room)
	room.StructureID = toStructureID
	return nil
}

// PlacePlayer adds a player to its current room's occupant list.
//
// Use this once per player when they first join a running world (e.g.
// on connect, using their persisted CurrentRoomID) -- after that, use
// MovePlayer for any change of room. Safe to call again for a player
// who's already placed (it's a no-op).
func (w *World) PlacePlayer(player *Player) error {
	room, err := w.GetRoom(player.CurrentRoomID)
	if err != nil {
		return err
	}
	if !containsPtr(room.Players, player) {
		room.Players = append(room.Players, player)
	}
	return nil
}

// MovePlayer moves a player to a different room.
//
// This is the *only* thing that should change which room a player is
// in: it updates player.CurrentRoomID and both rooms' occupant lists
// together. It only updates in-memory state -- persist the player too
// (e.g. PlayerRepository.Save) if the move should survive a restart.
func (w *World) MovePlayer(player *Player, toRoomID string) error {
	newRoom, err := w.GetRoom(toRoomID) // validate before mutating anything
	if err != nil {
		return err
	}
	if oldRoom, ok := w.rooms[player.CurrentRoomID]; ok {
		oldRoom.Players, _ = removePtr(oldRoom.Players, player)
	}
	newRoom.Players = append(newRoom.Players, player)
	player.CurrentRoomID = toRoomID
	return nil
}

// RemovePlayer removes a player from their current room's occupant
// list (e.g. on disconnect). Leaves player.CurrentRoomID untouched, so
// re-placing puts them back where they were.
func (w *World) RemovePlayer(player *Player) {
	if room, ok := w.rooms[player.CurrentRoomID]; ok {
		room.Players, _ = removePtr(room.Players, player)
	}
}

// RevivePlayer is what happens to a player who dies: they come to in
// the starting room, fully healed, still carrying everything. Like
// MovePlayer, in-memory only -- persist the player too if it should
// survive a restart.
func (w *World) RevivePlayer(player *Player) error {
	if player.Stats != nil {
		player.Stats.Restore()
	}
	return w.MovePlayer(player, w.StartingRoomID())
}

// SpawnNPC spawns a new instance of the npcID definition into roomID,
// the way loading a room's "npcs" list does. A wandering NPC spawned
// this way stands still: patrols are only started for the NPCs a world
// is loaded with.
func (w *World) SpawnNPC(npcID, roomID string) (*NPC, error) {
	definition, err := w.GetNPC(npcID)
	if err != nil {
		return nil, err
	}
	room, err := w.GetRoom(roomID)
	if err != nil {
		return nil, err
	}
	npc := definition.Spawn(roomID)
	room.NPCs = append(room.NPCs, npc)
	return npc, nil
}

// MoveNPC moves an already-spawned NPC to a different room. Mirrors
// MovePlayer exactly: it's the only thing that should change which
// room an NPC is in, updating npc.RoomID and both rooms' NPCs together.
func (w *World) MoveNPC(npc *NPC, toRoomID string) error {
	newRoom, err := w.GetRoom(toRoomID) // validate before mutating anything
	if err != nil {
		return err
	}
	if oldRoom, ok := w.rooms[npc.RoomID]; ok {
		oldRoom.NPCs, _ = removePtr(oldRoom.NPCs, npc)
	}
	newRoom.NPCs = append(newRoom.NPCs, npc)
	npc.RoomID = toRoomID
	return nil
}

// KillNPC removes a dead NPC from the world: everything it was carrying
// drops into its room (so `pick up` can loot it), it leaves the room's
// NPCs, and its RoomID is cleared -- which is also what stops a
// wandering NPC's patrol. Returns the dropped items.
func (w *World) KillNPC(npc *NPC) ([]*Item, error) {
	room, err := w.GetRoom(npc.RoomID)
	if err != nil {
		return nil, err
	}
	dropped := npc.Inventory
	room.Items = append(room.Items, dropped...)
	npc.Inventory = nil
	room.NPCs, _ = removePtr(room.NPCs, npc)
	npc.RoomID = ""
	return dropped, nil
}
