package llm

// Nothing here makes a real network call: NewAnthropicClient accepts an
// injected MessageCreator in place of the SDK's, and every test points
// MUD_LLM_CONFIG_FILE at a scratch path so it's never affected by -- or
// able to affect -- the real llm.config.json.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"mudengine/internal/logging/logtest"
)

const anthropicLog = "mud.llm.anthropic"

// isolate clears every MUD_LLM_* setting and points the config file at
// a scratch path that doesn't exist yet; it returns that path.
func isolate(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "llm.config.json")
	t.Setenv("MUD_LLM_CONFIG_FILE", path)
	for _, name := range []string{"MUD_LLM_PROVIDER", "MUD_LLM_API_KEY", "MUD_LLM_MODEL"} {
		t.Setenv(name, "")
	}
	return path
}

func writeConfig(t *testing.T, path string, data any) {
	t.Helper()
	raw, ok := data.(string)
	if !ok {
		encoded, _ := json.Marshal(data)
		raw = string(encoded)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeMessages stands in for the SDK's Messages service: records each
// request and returns a response decoded from responseJSON, or err.
type fakeMessages struct {
	responseJSON string
	err          error
	calls        []anthropic.MessageNewParams
}

func textResponse(text string, inputTokens, outputTokens int) string {
	data, _ := json.Marshal(map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"usage":   map[string]any{"input_tokens": inputTokens, "output_tokens": outputTokens},
	})
	return string(data)
}

func (f *fakeMessages) New(_ context.Context, body anthropic.MessageNewParams, _ ...option.RequestOption) (*anthropic.Message, error) {
	f.calls = append(f.calls, body)
	if f.err != nil {
		return nil, f.err
	}
	var message anthropic.Message
	if err := json.Unmarshal([]byte(f.responseJSON), &message); err != nil {
		return nil, err
	}
	return &message, nil
}

func TestAnthropicClientConstruction(t *testing.T) {
	isolate(t)

	if _, err := NewAnthropicClient(AnthropicOptions{}); !errors.Is(err, ErrConfig) {
		t.Errorf("no key, no injected client: got %v, want ErrConfig", err)
	}
	if _, err := NewAnthropicClient(AnthropicOptions{Messages: &fakeMessages{}}); err != nil {
		t.Errorf("injected client needs no key: %v", err)
	}
	if _, err := NewAnthropicClient(AnthropicOptions{APIKey: "sk-explicit"}); err != nil {
		t.Errorf("explicit key: %v", err)
	}
}

func TestAnthropicClientModel(t *testing.T) {
	path := isolate(t)
	fake := &fakeMessages{}

	client, _ := NewAnthropicClient(AnthropicOptions{Messages: fake})
	if client.Model() != DefaultAnthropicModel {
		t.Errorf("default model = %q", client.Model())
	}

	client, _ = NewAnthropicClient(AnthropicOptions{Messages: fake, Model: "claude-haiku-4-5"})
	if client.Model() != "claude-haiku-4-5" {
		t.Errorf("option model = %q", client.Model())
	}

	writeConfig(t, path, map[string]any{"model": "claude-from-file"})
	client, _ = NewAnthropicClient(AnthropicOptions{Messages: fake})
	if client.Model() != "claude-from-file" {
		t.Errorf("config file model = %q", client.Model())
	}

	// The env var takes precedence over the config file.
	t.Setenv("MUD_LLM_MODEL", "claude-from-env")
	client, _ = NewAnthropicClient(AnthropicOptions{Messages: fake})
	if client.Model() != "claude-from-env" {
		t.Errorf("env model = %q", client.Model())
	}
}

func TestConfigFile(t *testing.T) {
	path := isolate(t)

	if config, err := LoadConfigFile(); err != nil || len(config) != 0 {
		t.Errorf("missing file: %v, %v", config, err)
	}

	writeConfig(t, path, "{not json")
	if _, err := LoadConfigFile(); !errors.Is(err, ErrConfig) {
		t.Errorf("malformed: got %v", err)
	}

	writeConfig(t, path, "[1, 2, 3]")
	if _, err := LoadConfigFile(); !errors.Is(err, ErrConfig) {
		t.Errorf("non-object: got %v", err)
	}

	writeConfig(t, path, map[string]any{"api_key": "sk-from-file"})
	if _, err := NewAnthropicClient(AnthropicOptions{}); err != nil {
		t.Errorf("api key from file: %v", err)
	}

	// null fields fall through to the env var.
	writeConfig(t, path, map[string]any{"api_key": nil, "model": nil})
	t.Setenv("MUD_LLM_MODEL", "claude-from-env")
	client, _ := NewAnthropicClient(AnthropicOptions{Messages: &fakeMessages{}})
	if client.Model() != "claude-from-env" {
		t.Errorf("model = %q", client.Model())
	}
}

func TestGetClient(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		file    map[string]any
		wantErr bool
	}{
		{"default provider is anthropic", map[string]string{"MUD_LLM_API_KEY": "sk-test"}, nil, false},
		{"provider is case/whitespace insensitive", map[string]string{"MUD_LLM_PROVIDER": "  Anthropic  ", "MUD_LLM_API_KEY": "sk-test"}, nil, false},
		{"unknown provider", map[string]string{"MUD_LLM_PROVIDER": "some-other-llm", "MUD_LLM_API_KEY": "sk-test"}, nil, true},
		{"anthropic without api key", nil, nil, true},
		{"provider and key from config file", nil, map[string]any{"provider": "anthropic", "api_key": "sk-from-file"}, false},
		{"env provider overrides file provider", map[string]string{"MUD_LLM_PROVIDER": "anthropic", "MUD_LLM_API_KEY": "sk-test"}, map[string]any{"provider": "some-other-llm"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := isolate(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if tt.file != nil {
				writeConfig(t, path, tt.file)
			}

			client, err := GetClient()
			if tt.wantErr {
				if !errors.Is(err, ErrConfig) {
					t.Errorf("got %v, want ErrConfig", err)
				}
				return
			}
			if _, ok := client.(*AnthropicClient); !ok || err != nil {
				t.Errorf("got %T, %v", client, err)
			}
		})
	}
}

func TestComplete(t *testing.T) {
	isolate(t)
	fake := &fakeMessages{responseJSON: textResponse("a torch-lit room", 0, 0)}
	client, _ := NewAnthropicClient(AnthropicOptions{Messages: fake})

	out, err := client.Complete(context.Background(), "describe this room", "")
	if err != nil || out != "a torch-lit room" {
		t.Fatalf("got %q, %v", out, err)
	}
	call := fake.calls[0]
	if string(call.Model) != DefaultAnthropicModel || len(call.Messages) != 1 ||
		call.Messages[0].Role != anthropic.MessageParamRoleUser ||
		call.Messages[0].Content[0].OfText.Text != "describe this room" {
		t.Errorf("request = %+v", call)
	}
	if len(call.System) != 0 { // none given -> not sent at all
		t.Errorf("system = %+v", call.System)
	}

	client.Complete(context.Background(), "go north", "You are a command parser.")
	if system := fake.calls[1].System; len(system) != 1 || system[0].Text != "You are a command parser." {
		t.Errorf("system = %+v", system)
	}
}

func TestCompleteConcatenatesTextBlocks(t *testing.T) {
	isolate(t)
	fake := &fakeMessages{responseJSON: `{"content": [{"type": "text", "text": "hello "}, {"type": "text", "text": "world"}]}`}
	client, _ := NewAnthropicClient(AnthropicOptions{Messages: fake})

	if out, _ := client.Complete(context.Background(), "hi", ""); out != "hello world" {
		t.Errorf("got %q", out)
	}
}

func TestCompleteLogging(t *testing.T) {
	isolate(t)
	logs := logtest.Capture(t)
	client, _ := NewAnthropicClient(AnthropicOptions{Messages: &fakeMessages{responseJSON: textResponse("ok", 42, 7)}})

	client.Complete(context.Background(), "go north", "")

	record, ok := logs.Find(anthropicLog, "LLM call")
	if !ok || record.Attrs["input_tokens"] != "42" || record.Attrs["output_tokens"] != "7" ||
		record.Attrs["model"] != DefaultAnthropicModel {
		t.Errorf("records = %v", logs.Records(anthropicLog))
	}
}

func TestCompleteErrorIsWrappedAndLogged(t *testing.T) {
	isolate(t)
	logs := logtest.Capture(t)
	client, _ := NewAnthropicClient(AnthropicOptions{Messages: &fakeMessages{err: errors.New("rate limited")}})

	if _, err := client.Complete(context.Background(), "go north", ""); !errors.Is(err, ErrRequest) {
		t.Errorf("got %v, want ErrRequest", err)
	}
	record, ok := logs.Find(anthropicLog, "LLM call failed")
	if !ok || record.Level != slog.LevelWarn || !strings.Contains(record.Attrs["error"], "rate limited") {
		t.Errorf("records = %v", logs.Records(anthropicLog))
	}
}
