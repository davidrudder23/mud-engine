package world

// Loads a world (structures/rooms/npcs/items/doorways) from a directory
// tree of JSON files. Layout:
//
//	<world_dir>/structures/*.json   one Structure per file
//	<world_dir>/rooms/*.json        one Room per file
//	<world_dir>/npcs/*.json         one NPC per file (optional -- a world
//	                                with no NPCs just omits the directory)
//
// Structure file: {"id": "old-mill", "name": "Old Mill", "description": "..."}
// -- id and name required, description defaults to "".
//
// NPC file:
//
//	{
//	  "id": "old-tom", "name": "Old Tom", "description": "...",
//	  "hostile": false, "commands": ["trade"],
//	  "inventory": [{"id": "rag", "name": "a bar rag", "description": "..."}],
//	  "type": "wandering", "move_interval": 120
//	}
//
// id and name required; everything else optional. "type" is "npc" (the
// default) or "wandering"; move_interval (seconds) is only read for
// "wandering". An unrecognized type is a load error.
//
// Room file:
//
//	{
//	  "id": "cave-entrance", "structure_id": "old-mill", "description": "...",
//	  "items": [{"id": "torch", "name": "a torch", "description": "Still lit."}],
//	  "doorways": [{"direction": "north", "to": "cave-tunnel", "description": "..."}],
//	  "npcs": ["old-tom"],
//	  "commands": ["light torch"]
//	}
//
// id and structure_id required; everything else defaults to empty. An
// item's name defaults to its id. Doorway targets, structure_ids, and
// npcs references are validated only once every file has been read, so
// files can reference each other regardless of load order.
//
// Unlike items (inline, one-off per room), npcs is a list of ids
// referencing npcs/*.json. Each id in World.NPCs is the definition,
// loaded once; every room reference spawns a fresh, independent
// NPC.Spawn instance into that room -- the same id can appear in many
// rooms, or more than once in one room.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// LoadError is returned when the on-disk world data is malformed or
// inconsistent.
type LoadError struct {
	Msg string
}

func (e *LoadError) Error() string {
	return "world load: " + e.Msg
}

func loadErrorf(format string, args ...any) error {
	return &LoadError{Msg: fmt.Sprintf(format, args...)}
}

type itemJSON struct {
	ID          *string `json:"id"`
	Name        *string `json:"name"`
	Description string  `json:"description"`
}

type doorwayJSON struct {
	Direction   *string `json:"direction"`
	To          *string `json:"to"`
	Description string  `json:"description"`
}

type structureJSON struct {
	ID          *string `json:"id"`
	Name        *string `json:"name"`
	Description string  `json:"description"`
}

type npcJSON struct {
	ID           *string    `json:"id"`
	Name         *string    `json:"name"`
	Description  string     `json:"description"`
	Hostile      bool       `json:"hostile"`
	Commands     []string   `json:"commands"`
	Inventory    []itemJSON `json:"inventory"`
	Type         *string    `json:"type"`
	MoveInterval *float64   `json:"move_interval"`
}

type roomJSON struct {
	ID          *string       `json:"id"`
	StructureID *string       `json:"structure_id"`
	Description string        `json:"description"`
	Items       []itemJSON    `json:"items"`
	Doorways    []doorwayJSON `json:"doorways"`
	NPCs        []string      `json:"npcs"`
	Commands    []string      `json:"commands"`
}

// npcTypes lists the recognized NPC "type" values ("npc" is the
// default when the field is omitted).
var npcTypes = []string{"npc", "wandering"}

// jsonFiles returns dir's *.json files, sorted by name.
func jsonFiles(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func decodeFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return loadErrorf("%s: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return loadErrorf("%s: invalid JSON (%v)", path, err)
	}
	return nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func itemFromJSON(data itemJSON, source string) (*Item, error) {
	if data.ID == nil {
		return nil, loadErrorf("%s: item is missing required field 'id'", source)
	}
	item := &Item{ID: *data.ID, Name: *data.ID, Description: data.Description}
	if data.Name != nil {
		item.Name = *data.Name
	}
	return item, nil
}

func itemsFromJSON(data []itemJSON, source string) ([]*Item, error) {
	items := make([]*Item, 0, len(data))
	for _, d := range data {
		item, err := itemFromJSON(d, source)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func structureFromFile(path string) (*Structure, error) {
	var data structureJSON
	if err := decodeFile(path, &data); err != nil {
		return nil, err
	}
	if data.ID == nil {
		return nil, loadErrorf("%s: structure is missing required field 'id'", path)
	}
	if data.Name == nil {
		return nil, loadErrorf("%s: structure is missing required field 'name'", path)
	}
	return &Structure{ID: *data.ID, Name: *data.Name, Description: data.Description}, nil
}

func npcFromFile(path string) (*NPC, error) {
	var data npcJSON
	if err := decodeFile(path, &data); err != nil {
		return nil, err
	}
	if data.ID == nil {
		return nil, loadErrorf("%s: npc is missing required field 'id'", path)
	}
	if data.Name == nil {
		return nil, loadErrorf("%s: npc is missing required field 'name'", path)
	}

	npcType := "npc"
	if data.Type != nil {
		npcType = *data.Type
	}
	if npcType != "npc" && npcType != "wandering" {
		return nil, loadErrorf(
			"%s: npc %q has unknown type %q (expected one of %q)",
			path, *data.ID, npcType, npcTypes,
		)
	}

	inventory, err := itemsFromJSON(data.Inventory, path)
	if err != nil {
		return nil, err
	}
	npc := &NPC{
		ID:          *data.ID,
		Name:        *data.Name,
		Description: data.Description,
		Hostile:     data.Hostile,
		Commands:    append([]string{}, data.Commands...),
		Inventory:   inventory,
	}
	if npcType == "wandering" {
		npc.Wander = &Wander{MoveInterval: DefaultMoveInterval}
		if data.MoveInterval != nil {
			npc.Wander.MoveInterval = time.Duration(*data.MoveInterval * float64(time.Second))
		}
	}
	return npc, nil
}

func roomFromFile(path string) (*Room, []string, error) {
	var data roomJSON
	if err := decodeFile(path, &data); err != nil {
		return nil, nil, err
	}
	if data.ID == nil {
		return nil, nil, loadErrorf("%s: room is missing required field 'id'", path)
	}
	if data.StructureID == nil {
		return nil, nil, loadErrorf("%s: room is missing required field 'structure_id'", path)
	}

	items, err := itemsFromJSON(data.Items, path)
	if err != nil {
		return nil, nil, err
	}
	doorways := make([]*Doorway, 0, len(data.Doorways))
	for _, d := range data.Doorways {
		if d.Direction == nil || d.To == nil {
			return nil, nil, loadErrorf("%s: doorway is missing required field 'direction' or 'to'", path)
		}
		doorways = append(doorways, &Doorway{Direction: *d.Direction, To: *d.To, Description: d.Description})
	}

	room := &Room{
		ID:          *data.ID,
		StructureID: *data.StructureID,
		Description: data.Description,
		Items:       items,
		Doorways:    doorways,
		Commands:    append([]string{}, data.Commands...),
	}
	return room, data.NPCs, nil
}

// Load reads <worldDir>/structures/*.json, <worldDir>/rooms/*.json,
// and (if present) <worldDir>/npcs/*.json into a World.
//
// Returns a *LoadError if a file is malformed, an id is duplicated, a
// doorway points at an unknown room, a room references an unknown
// structure_id or npc id, or an npc has an unrecognized type.
func Load(worldDir string) (*World, error) {
	structuresDir := filepath.Join(worldDir, "structures")
	roomsDir := filepath.Join(worldDir, "rooms")
	npcsDir := filepath.Join(worldDir, "npcs")

	if !isDir(structuresDir) {
		return nil, loadErrorf("not a directory: %s", structuresDir)
	}
	if !isDir(roomsDir) {
		return nil, loadErrorf("not a directory: %s", roomsDir)
	}
	// npcsDir is optional -- a world with no NPCs at all just omits it.

	w := New()

	paths, err := jsonFiles(structuresDir)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		structure, err := structureFromFile(path)
		if err != nil {
			return nil, err
		}
		if _, dup := w.Structures[structure.ID]; dup {
			return nil, loadErrorf("%s: duplicate structure id %q (already defined by another file)", path, structure.ID)
		}
		w.Structures[structure.ID] = structure
	}

	if isDir(npcsDir) {
		paths, err := jsonFiles(npcsDir)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			npc, err := npcFromFile(path)
			if err != nil {
				return nil, err
			}
			if _, dup := w.NPCs[npc.ID]; dup {
				return nil, loadErrorf("%s: duplicate npc id %q (already defined by another file)", path, npc.ID)
			}
			w.NPCs[npc.ID] = npc
		}
	}

	// Room "npcs" entries are ids, resolved below once every npcs/*.json
	// has been read -- stashed here (keyed by room id) rather than on
	// Room itself, since Room.NPCs holds actual NPCs, not ids.
	roomNPCIDs := map[string][]string{}

	paths, err = jsonFiles(roomsDir)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		room, npcIDs, err := roomFromFile(path)
		if err != nil {
			return nil, err
		}
		if w.HasRoom(room.ID) {
			return nil, loadErrorf("%s: duplicate room id %q (already defined by another file)", path, room.ID)
		}
		w.AddRoom(room)
		roomNPCIDs[room.ID] = npcIDs
	}

	// Validate & link only after everything is loaded, so file order
	// doesn't matter.
	for _, room := range w.Rooms() {
		for _, doorway := range room.Doorways {
			if !w.HasRoom(doorway.To) {
				return nil, loadErrorf(
					"room %q has a doorway (%q) pointing to unknown room %q",
					room.ID, doorway.Direction, doorway.To,
				)
			}
		}

		if _, ok := w.Structures[room.StructureID]; !ok {
			return nil, loadErrorf("room %q belongs to unknown structure %q", room.ID, room.StructureID)
		}

		for _, npcID := range roomNPCIDs[room.ID] {
			definition, ok := w.NPCs[npcID]
			if !ok {
				return nil, loadErrorf("room %q references unknown npc %q", room.ID, npcID)
			}
			// A fresh instance per reference, not the shared definition.
			room.NPCs = append(room.NPCs, definition.Spawn(room.ID))
		}

		if err := w.PlaceRoom(room); err != nil {
			return nil, err
		}
	}

	return w, nil
}
