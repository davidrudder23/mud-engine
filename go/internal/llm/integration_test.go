package llm

import (
	"context"
	"strings"
	"testing"
)

// TestCanReachTheConfiguredLLM makes one real request to whichever
// provider is configured, to confirm network, API key, and model name
// all work together. Skipped unless an API key is configured
// (MUD_LLM_API_KEY, or "api_key" in the config file -- run from the
// project root, or set MUD_LLM_CONFIG_FILE, for the file to be found).
// Costs a small number of tokens when it runs.
func TestCanReachTheConfiguredLLM(t *testing.T) {
	if key, err := Setting("MUD_LLM_API_KEY", "api_key"); err != nil || key == "" {
		t.Skip(`no LLM API key configured -- set MUD_LLM_API_KEY or "api_key" in llm.config.json to run this test`)
	}

	client, err := GetClient()
	if err != nil {
		t.Fatalf("could not build the configured LLM client: %v", err)
	}
	reply, err := client.Complete(context.Background(),
		"Reply with exactly one word: PONG",
		"You are being used to test API connectivity. Follow the instruction exactly -- reply with nothing else.",
	)
	if err != nil {
		t.Fatalf("could not reach the configured LLM: %v", err)
	}
	if !strings.Contains(strings.ToUpper(reply), "PONG") {
		t.Errorf("reply = %q", reply)
	}
}
