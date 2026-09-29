package commands

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"mudengine/internal/llm"
	"mudengine/internal/llm/llmtest"
	"mudengine/internal/logging/logtest"
	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

const llmRunnerLog = "mud.commands.llm_runner"

func reply(command any, parameters map[string]string) string {
	if parameters == nil {
		parameters = map[string]string{}
	}
	data, _ := json.Marshal(map[string]any{"command": command, "parameters": parameters})
	return string(data)
}

// llmFixture wires look/go/quit/pick up -- enough variety to exercise
// no-parameter, required-parameter, and multi-word-verb commands.
func llmFixture(t *testing.T) (*fixture, *llmtest.FakeClient, *LLMCommandRunner) {
	f := newFixture(t)
	fake := &llmtest.FakeClient{}
	runner := NewLLMCommandRunner(fake, []Command{
		NewLookCommand(f.sessions, f.world),
		NewGoCommand(f.sessions, f.world, f.repo),
		NewQuitCommand(f.sessions),
		NewPickUpCommand(f.sessions, f.world),
	})
	return f, fake, runner
}

func TestBuildSystemPrompt(t *testing.T) {
	f := newFixture(t)
	prompt := BuildSystemPrompt([]Command{
		NewLookCommand(f.sessions, f.world),
		NewGoCommand(f.sessions, f.world, f.repo),
	})
	for _, want := range []string{
		`"look"`, `"go"`,
		"none",                         // look takes no parameters
		"direction (string, required)", // go's parameter
		"JSON object", `"command"`, `"parameters"`,
		"re-describes the room you're in", // describe text, whitespace collapsed
		"Also typed as: leave, exit, travel.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestLLMRunnerSendsLineAsPromptWithSystemPrompt(t *testing.T) {
	f, fake, runner := llmFixture(t)
	fake.Reply = reply("look", nil)
	f.runLine(t, runner, "take a look around")
	if fake.Calls[0].Prompt != "take a look around" || fake.Calls[0].System == "" {
		t.Errorf("call = %+v", fake.Calls[0])
	}
}

func TestLLMRunnerDispatch(t *testing.T) {
	f, fake, runner := llmFixture(t)

	fake.Reply = reply("look", nil)
	if out := f.runLine(t, runner, "take a look around"); !strings.Contains(out, "The starting room.") {
		t.Errorf("look: %q", out)
	}

	// The model matched go but found no direction -- the runner doesn't
	// invent one; go's own prompt handles the gap.
	fake.Reply = reply("go", nil)
	if out := f.runLine(t, runner, "which way do I go?"); out != "Go where?" || f.player.CurrentRoomID != worldtest.StartID {
		t.Errorf("go without direction: %q", out)
	}

	fake.Reply = reply("go", map[string]string{"direction": "north"})
	if out := f.runLine(t, runner, "head north please"); f.player.CurrentRoomID != worldtest.NorthID ||
		out != f.room(worldtest.NorthID).Format() {
		t.Errorf("go north: %q", out)
	}
}

func TestLLMRunnerMultiWordVerbAndArgument(t *testing.T) {
	f, fake, runner := llmFixture(t)
	sword := &world.Item{ID: "sword", Name: "a rusty sword"}
	start := f.room(worldtest.StartID)
	start.Items = append(start.Items, sword)
	fake.Reply = reply("pick up", map[string]string{"item": "rusty sword"})

	if out := f.runLine(t, runner, "grab that rusty sword"); out != "You pick up a rusty sword." ||
		!slices.Contains(f.player.Inventory, sword) {
		t.Errorf("got %q", out)
	}
}

func TestLLMRunnerIgnoresUndeclaredParameters(t *testing.T) {
	f, fake, runner := llmFixture(t)
	fake.Reply = `{"command": "go", "parameters": {"direction": "north", "speed": "fast"}}`
	if out := f.runLine(t, runner, "run north fast"); out != f.room(worldtest.NorthID).Format() {
		t.Errorf("got %q", out)
	}
}

func TestLLMRunnerStripsCodeFence(t *testing.T) {
	f, fake, runner := llmFixture(t)
	fake.Reply = "```json\n" + reply("look", nil) + "\n```"
	if out := f.runLine(t, runner, "look"); !strings.Contains(out, "The starting room.") {
		t.Errorf("got %q", out)
	}
}

func TestLLMRunnerFailuresAreUnknownCommand(t *testing.T) {
	tests := []struct {
		name, reply, line string
		err               error
	}{
		{"null command", reply(nil, nil), "dance", nil},
		{"hallucinated command", reply("fly", nil), "fly away", nil},
		{"malformed json", "not json at all", "look", nil},
		{"non-object json", "[1, 2, 3]", "look", nil},
		{"request error", "", "look", fmt.Errorf("%w: provider down", llm.ErrRequest)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, fake, runner := llmFixture(t)
			fake.Reply, fake.Err = tt.reply, tt.err
			if out := f.runLine(t, runner, tt.line); out != UnknownCommand(tt.line) {
				t.Errorf("got %q", out)
			}
		})
	}
}

func TestLLMRunnerLogging(t *testing.T) {
	tests := []struct {
		name, reply string
		err         error
		level       slog.Level
		message     string
	}{
		{"request error warns", "", fmt.Errorf("%w: provider down", llm.ErrRequest), slog.LevelWarn, "LLM request failed"},
		{"malformed reply warns", "not json at all", nil, slog.LevelWarn, "Could not parse LLM reply"},
		// A well-formed "no command applies" isn't a parsing failure.
		{"null command is info", reply(nil, nil), nil, slog.LevelInfo, "LLM found no matching command"},
		{"hallucinated command is info", reply("fly", nil), nil, slog.LevelInfo, "LLM matched line to unregistered command"},
		{"match is debug", reply("go", map[string]string{"direction": "north"}), nil, slog.LevelDebug, "LLM matched command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture only what Run logs, not building the runner.
			f, fake, runner := llmFixture(t)
			logs := logtest.Capture(t)
			fake.Reply, fake.Err = tt.reply, tt.err

			f.runLine(t, runner, "some line")

			records := logs.Records(llmRunnerLog)
			if len(records) != 1 || records[0].Message != tt.message || records[0].Level != tt.level {
				t.Errorf("records = %v", records)
			}
			if tt.err != nil && !strings.Contains(records[0].Attrs["error"], "provider down") {
				t.Errorf("error attr = %q", records[0].Attrs["error"])
			}
		})
	}
}

func TestLLMRunnerWithHelp(t *testing.T) {
	f := newFixture(t)
	list := []Command{NewLookCommand(f.sessions, f.world)}
	list = append(list, NewHelpCommand(f.sessions, f.world, &list))
	fake := &llmtest.FakeClient{Reply: reply("help", nil)}
	runner := NewLLMCommandRunner(fake, list)

	out := f.runLine(t, runner, "what can I do?")
	if !strings.Contains(out, "look") || !strings.Contains(out, "help") {
		t.Errorf("got %q", out)
	}
}
