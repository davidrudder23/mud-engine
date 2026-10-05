package mcpserver

// Drives a real MCP server through a real MCP client over in-memory
// transports -- these exercise the actual schema and results a client
// sees, not just that the handler returns the right struct.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mudengine/internal/llm"
	"mudengine/internal/repository/repotest"
	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

type fixture struct {
	world   *world.World
	repo    *repotest.FakePlayerRepository
	client  *mcp.ClientSession
	startRm *world.Room
}

func newFixture(t *testing.T, opts Options) *fixture {
	t.Helper()
	ctx := context.Background()
	f := &fixture{world: worldtest.TwoRooms(), repo: repotest.New()}
	f.startRm = worldtest.MustRoom(f.world, worldtest.StartID)

	server, err := BuildServer(ctx, f.world, f.repo, "alice", opts)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	f.client, err = client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.client.Close() })
	return f
}

func (f *fixture) tools(t *testing.T) map[string]*mcp.Tool {
	t.Helper()
	result, err := f.client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range result.Tools {
		tools[tool.Name] = tool
	}
	return tools
}

func (f *fixture) call(t *testing.T, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	result, err := f.client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result
}

// structured decodes a result's structured content the way a client
// sees it.
func structured(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool error: %+v", result.Content)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func roomOf(t *testing.T, result *mcp.CallToolResult) map[string]any {
	return structured(t, result)["room"].(map[string]any)
}

func asJSON(t *testing.T, v any) any {
	data, _ := json.Marshal(v)
	var out any
	json.Unmarshal(data, &out)
	return out
}

func TestRegistersOneToolPerCommand(t *testing.T) {
	f := newFixture(t, Options{})
	tools := f.tools(t)
	var names []string
	for name := range tools {
		names = append(names, name)
	}
	want := []string{"look", "go", "quit", "pick_up", "drop", "inventory", "stats", "attack", "order", "talk", "help"}
	if len(names) != len(want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
	for _, name := range want {
		if tools[name] == nil {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestToolDescriptionCollapsesWhitespace(t *testing.T) {
	f := newFixture(t, Options{})
	if got := f.tools(t)["look"].Description; got != "look re-describes the room you're in" {
		t.Errorf("description = %q", got)
	}
}

func TestToolSchemas(t *testing.T) {
	f := newFixture(t, Options{})
	tools := f.tools(t)

	look := tools["look"].InputSchema.(map[string]any)
	if props := look["properties"].(map[string]any); len(props) != 0 {
		t.Errorf("look properties = %v", props)
	}
	if _, ok := look["required"]; ok {
		t.Errorf("look has required: %v", look)
	}

	goSchema := tools["go"].InputSchema.(map[string]any)
	direction := goSchema["properties"].(map[string]any)["direction"].(map[string]any)
	if direction["type"] != "string" || !reflect.DeepEqual(goSchema["required"], []any{"direction"}) {
		t.Errorf("go schema = %v", goSchema)
	}
}

func TestResultShape(t *testing.T) {
	f := newFixture(t, Options{})
	result := f.call(t, "look", nil)

	content := structured(t, result)
	if len(content) != 2 || content["message"] == nil || content["room"] == nil {
		t.Errorf("structured content = %v", content)
	}
	if !reflect.DeepEqual(content["room"], asJSON(t, f.startRm.View())) {
		t.Errorf("room = %v", content["room"])
	}
	// The same JSON is readable as a text content block too.
	if len(result.Content) != 1 {
		t.Fatalf("content = %v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"message"`) || !strings.Contains(text, `"room"`) {
		t.Errorf("text = %q", text)
	}

	if message := structured(t, f.call(t, "quit", nil))["message"]; message != "" {
		t.Errorf("quit message = %q", message)
	}
}

func TestGoMovesAndPersists(t *testing.T) {
	f := newFixture(t, Options{})

	result := f.call(t, "go", map[string]any{"direction": "west"})
	if structured(t, result)["message"] != "You can't go 'west' from here." || roomOf(t, result)["id"] != worldtest.StartID {
		t.Errorf("invalid direction: %v", structured(t, result))
	}

	result = f.call(t, "go", map[string]any{"direction": "north"})
	if roomOf(t, result)["id"] != worldtest.NorthID {
		t.Errorf("room = %v", roomOf(t, result)["id"])
	}
	if saved, _ := f.repo.GetByName(context.Background(), "alice"); saved.CurrentRoomID != worldtest.NorthID {
		t.Errorf("saved room = %q", saved.CurrentRoomID)
	}
}

func TestPickUpAndDropRoundTrip(t *testing.T) {
	f := newFixture(t, Options{})
	f.startRm.Items = append(f.startRm.Items, &world.Item{ID: "rock", Name: "a rock"})

	if items := roomOf(t, f.call(t, "pick_up", map[string]any{"item": "rock"}))["items"].([]any); len(items) != 0 {
		t.Errorf("after pick up: %v", items)
	}
	if items := roomOf(t, f.call(t, "drop", map[string]any{"item": "rock"}))["items"].([]any); len(items) != 1 {
		t.Errorf("after drop: %v", items)
	}
}

func TestTalkWithoutLLMConfigFallsBackGracefully(t *testing.T) {
	// talk resolves its client for real via the llm package -- isolate
	// that from any real config so this can't make a network call.
	t.Setenv("MUD_LLM_CONFIG_FILE", filepath.Join(t.TempDir(), "llm.config.json"))
	t.Setenv("MUD_LLM_API_KEY", "")
	t.Setenv("MUD_LLM_PROVIDER", "")
	f := newFixture(t, Options{TalkLLM: llm.GetClient})
	f.startRm.NPCs = append(f.startRm.NPCs, &world.NPC{ID: "old-tom", Name: "Old Tom"})

	result := f.call(t, "talk", map[string]any{"npc": "tom"})
	if message := structured(t, result)["message"]; message != "Old Tom doesn't seem to hear you." {
		t.Errorf("message = %q", message)
	}
}

func TestHelpListsEveryCommand(t *testing.T) {
	f := newFixture(t, Options{})
	message := structured(t, f.call(t, "help", nil))["message"].(string)
	for _, verb := range []string{"look", "go <dir>", "quit", "pick up <item>", "drop <item>"} {
		if !strings.Contains(message, verb) {
			t.Errorf("help missing %q", verb)
		}
	}
}

func TestBadArguments(t *testing.T) {
	f := newFixture(t, Options{})
	if result := f.call(t, "go", nil); !result.IsError {
		t.Error("missing required argument accepted")
	}
	if result := f.call(t, "go", map[string]any{"direction": 3}); !result.IsError {
		t.Error("non-string argument accepted")
	}
}

func TestUnknownToolIsRejected(t *testing.T) {
	f := newFixture(t, Options{})
	_, err := f.client.CallTool(context.Background(), &mcp.CallToolParams{Name: "dance", Arguments: map[string]any{}})
	if err == nil {
		t.Error("unknown tool accepted")
	}
}
