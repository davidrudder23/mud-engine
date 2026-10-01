package llm

// LLM provider selection: environment variables first, then a config
// file, so settings don't have to live in the shell environment if
// you'd rather not.
//
// Precedence (highest wins): an explicit option (if you build a client
// directly) > environment variable > config file > each provider's own
// built-in default.
//
//	MUD_LLM_PROVIDER     which Client implementation GetClient builds.
//	MUD_LLM_API_KEY      the selected provider's API key. Deliberately
//	                     generic rather than e.g. ANTHROPIC_API_KEY, so
//	                     switching providers doesn't mean renaming vars.
//	MUD_LLM_MODEL        optional; each provider has its own default.
//	MUD_LLM_CONFIG_FILE  where the config file lives; default
//	                     "llm.config.json" in the working directory (the
//	                     project root, when run via run.sh).
//
// The config file holds the same three settings as a JSON object:
//
//	{"provider": "anthropic", "api_key": null, "model": null}
//
// It's optional -- missing entirely is treated the same as every field
// being null -- and any field left null/omitted falls through to the
// env var, then the built-in default. It's gitignored, since api_key
// is a secret once you fill it in.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

const (
	DefaultProvider   = "anthropic"
	DefaultConfigFile = "llm.config.json"
)

// ConfigFile returns the config file path in effect.
func ConfigFile() string {
	if path := os.Getenv("MUD_LLM_CONFIG_FILE"); path != "" {
		return path
	}
	return DefaultConfigFile
}

// LoadConfigFile reads the config file into a map, or an empty map if
// it doesn't exist. Returns an error wrapping ErrConfig if it exists
// but isn't a valid JSON object.
func LoadConfigFile() (map[string]any, error) {
	path := ConfigFile()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrConfig, path, err)
	}

	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("%w: %s: invalid JSON (%v)", ErrConfig, path, err)
	}
	object, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s: expected a JSON object, got %T", ErrConfig, path, parsed)
	}
	return object, nil
}

// Setting returns one setting's value: the environment variable if
// set, else the config file's value (treating null/missing/non-string
// as unset), else "" -- the caller applies whatever default makes
// sense for that setting.
func Setting(envVar, fileKey string) (string, error) {
	if value := os.Getenv(envVar); value != "" {
		return value, nil
	}
	config, err := LoadConfigFile()
	if err != nil {
		return "", err
	}
	value, _ := config[fileKey].(string)
	return value, nil
}

// GetClient builds the Client selected by config.
func GetClient() (Client, error) {
	provider, err := Setting("MUD_LLM_PROVIDER", "provider")
	if err != nil {
		return nil, err
	}
	if provider == "" {
		provider = DefaultProvider
	}
	provider = strings.ToLower(strings.TrimSpace(provider))

	switch provider {
	case "anthropic":
		return NewAnthropicClient(AnthropicOptions{})
	default:
		return nil, fmt.Errorf("%w: unknown MUD_LLM_PROVIDER: %q (expected: \"anthropic\")", ErrConfig, provider)
	}
}
