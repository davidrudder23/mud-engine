package world

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestItemPlayerNPCCommands(t *testing.T) {
	if got := (&Item{ID: "sword", Name: "a sword"}).GetCommands(); !slices.Equal(got, []string{"pick up"}) {
		t.Errorf("item: %v", got)
	}
	if got := (&Player{Name: "alice"}).GetCommands(); len(got) != 0 {
		t.Errorf("player: %v", got)
	}
	if got := (&NPC{ID: "old-tom", Name: "Old Tom"}).GetCommands(); !slices.Equal(got, []string{"talk"}) {
		t.Errorf("npc: %v", got)
	}
}

func TestRoomGetCommands(t *testing.T) {
	north := &Doorway{Direction: "north", To: "elsewhere"}
	tests := []struct {
		name string
		room Room
		want []string
	}{
		{"empty room only offers look", Room{}, []string{"look"}},
		{"doorway adds go", Room{Doorways: []*Doorway{north}}, []string{"look", "go"}},
		{"item adds its commands", Room{Items: []*Item{{ID: "sword"}}}, []string{"look", "pick up"}},
		{"npc adds its commands", Room{NPCs: []*NPC{{ID: "old-tom", Commands: []string{"trade"}}}}, []string{"look", "talk", "trade"}},
		// Player.GetCommands is empty for now, so this is a no-op.
		{"player adds its commands", Room{Players: []*Player{{Name: "alice"}}}, []string{"look"}},
		{"shared item commands dedupe", Room{Items: []*Item{{ID: "sword"}, {ID: "shield"}}}, []string{"look", "pick up"}},
		{
			"everything combined",
			Room{
				Doorways: []*Doorway{north},
				Items:    []*Item{{ID: "sword"}},
				NPCs:     []*NPC{{ID: "old-tom"}},
				Players:  []*Player{{Name: "alice"}},
			},
			[]string{"look", "go", "pick up", "talk"},
		},
		{"json commands are added", Room{Commands: []string{"pray", "ring bell"}}, []string{"look", "pray", "ring bell"}},
		{"json commands dedupe against defaults", Room{Doorways: []*Doorway{north}, Commands: []string{"go", "pray"}}, []string{"look", "go", "pray"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.room.GetCommands(); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRoomFormat(t *testing.T) {
	if strings.Contains((&Room{}).Format(), "NPCs here:") {
		t.Error("NPCs section shown for a room with none")
	}

	room := &Room{ID: "room", NPCs: []*NPC{{ID: "old-tom", Name: "Old Tom"}}}
	lines := strings.Split(room.Format(), "\r\n")
	if !slices.Contains(lines, "NPCs here:") || !slices.Contains(lines, "  - Old Tom") {
		t.Errorf("lines = %q", lines)
	}
}

func TestRoomFormatFull(t *testing.T) {
	room := &Room{
		ID:          "r",
		Description: "A room.",
		Items:       []*Item{{Name: "a sword"}},
		NPCs:        []*NPC{{Name: "Old Tom"}},
		Players:     []*Player{{Name: "alice"}},
		Doorways:    []*Doorway{{Direction: "north", Description: "Narrow."}, {Direction: "south"}},
	}
	want := strings.Join([]string{
		"[r]", "A room.",
		"You see:", "  - a sword",
		"NPCs here:", "  - Old Tom",
		"Players here:", "  - alice",
		"Doorways:", "  - north (Narrow.)", "  - south",
	}, "\r\n")
	if got := room.Format(); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// asJSON round-trips v through encoding/json, for comparing views the
// way a client would see them.
func asJSON(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestViews(t *testing.T) {
	tests := []struct {
		name string
		view any
		want string
	}{
		{
			"item",
			(&Item{ID: "sword", Name: "a sword", Description: "Rusty."}).View(),
			`{"id": "sword", "name": "a sword", "description": "Rusty."}`,
		},
		{
			"doorway",
			(&Doorway{Direction: "north", To: "elsewhere", Description: "Narrow."}).View(),
			`{"direction": "north", "to": "elsewhere", "description": "Narrow."}`,
		},
		{
			"npc with nested inventory",
			(&NPC{
				ID: "old-tom", Name: "Old Tom", Description: "A bartender.", Hostile: true,
				Commands: []string{"trade"}, Inventory: []*Item{{ID: "rag", Name: "a rag"}},
			}).View(),
			`{"id": "old-tom", "name": "Old Tom", "description": "A bartender.", "hostile": true,
			  "commands": ["trade"], "inventory": [{"id": "rag", "name": "a rag", "description": ""}]}`,
		},
		{
			"wandering npc includes move_interval",
			(&NPC{ID: "watchman", Name: "A Watchman", Wander: &Wander{MoveInterval: 45 * time.Second}}).View(),
			`{"id": "watchman", "name": "A Watchman", "description": "", "hostile": false,
			  "commands": [], "inventory": [], "move_interval": 45}`,
		},
		{
			"unpersisted player has null id",
			(&Player{Name: "alice", Inventory: []*Item{{ID: "rock", Name: "a rock"}}}).View(),
			`{"id": null, "name": "alice", "inventory": [{"id": "rock", "name": "a rock", "description": ""}]}`,
		},
		{
			"empty room",
			(&Room{ID: "room", StructureID: "structure", Description: "A room."}).View(),
			`{"id": "room", "structure_id": "structure", "description": "A room.",
			  "items": [], "npcs": [], "players": [], "doorways": [], "commands": ["look"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var want any
			if err := json.Unmarshal([]byte(tt.want), &want); err != nil {
				t.Fatal(err)
			}
			if got := asJSON(t, tt.view); !reflect.DeepEqual(got, want) {
				t.Errorf("got %v\nwant %v", got, want)
			}
		})
	}
}

func TestPlayerViewStringifiesID(t *testing.T) {
	id := uuid.New()
	view := (&Player{ID: id, Name: "alice"}).View()
	if view.ID == nil || *view.ID != id.String() {
		t.Errorf("ID = %v, want %s", view.ID, id)
	}
}

func TestRoomViewNestsElementViewsAndCommands(t *testing.T) {
	room := &Room{
		ID:       "room",
		Items:    []*Item{{ID: "sword", Name: "a sword"}},
		NPCs:     []*NPC{{ID: "old-tom", Name: "Old Tom"}},
		Players:  []*Player{{Name: "alice"}},
		Doorways: []*Doorway{{Direction: "north", To: "elsewhere"}},
	}
	view := room.View()
	if !reflect.DeepEqual(view.Items, []ItemView{room.Items[0].View()}) ||
		!reflect.DeepEqual(view.NPCs, []NPCView{room.NPCs[0].View()}) ||
		!reflect.DeepEqual(view.Players, []PlayerView{room.Players[0].View()}) ||
		!reflect.DeepEqual(view.Doorways, []DoorwayView{room.Doorways[0].View()}) {
		t.Errorf("view = %+v", view)
	}
	if !slices.Equal(view.Commands, room.GetCommands()) {
		t.Errorf("Commands = %v, want GetCommands() %v", view.Commands, room.GetCommands())
	}
}
