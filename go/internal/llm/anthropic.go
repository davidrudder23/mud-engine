package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"mudengine/internal/logging"
)

const (
	// Per-turn look-ups over one typed line are a small, well-defined
	// task, so the cheaper/faster end of the current lineup is the
	// sensible default; override with MUD_LLM_MODEL if needed.
	DefaultAnthropicModel = "claude-sonnet-5"
	defaultMaxTokens      = 1024
)

var anthropicLogger = logging.For("mud.llm.anthropic")

// MessageCreator is the one Anthropic SDK method AnthropicClient uses
// (satisfied by &anthropic.Client.Messages), so tests can inject a
// fake instead of making network calls.
type MessageCreator interface {
	New(ctx context.Context, body anthropic.MessageNewParams, opts ...option.RequestOption) (*anthropic.Message, error)
}

// AnthropicOptions overrides config for NewAnthropicClient. Every field
// is optional.
type AnthropicOptions struct {
	APIKey string
	Model  string
	// Messages replaces the real SDK client -- for tests only.
	Messages MessageCreator
}

// AnthropicClient is the Claude implementation of Client.
type AnthropicClient struct {
	model    string
	messages MessageCreator
}

func NewAnthropicClient(opts AnthropicOptions) (*AnthropicClient, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		var err error
		if apiKey, err = Setting("MUD_LLM_API_KEY", "api_key"); err != nil {
			return nil, err
		}
	}
	if apiKey == "" && opts.Messages == nil {
		return nil, fmt.Errorf(
			"%w: no Anthropic API key found: set MUD_LLM_API_KEY, or \"api_key\" in %s",
			ErrConfig, ConfigFile(),
		)
	}

	model := opts.Model
	if model == "" {
		var err error
		if model, err = Setting("MUD_LLM_MODEL", "model"); err != nil {
			return nil, err
		}
	}
	if model == "" {
		model = DefaultAnthropicModel
	}

	messages := opts.Messages
	if messages == nil {
		client := anthropic.NewClient(option.WithAPIKey(apiKey))
		messages = &client.Messages
	}
	return &AnthropicClient{model: model, messages: messages}, nil
}

// Model is the model this client sends requests to.
func (c *AnthropicClient) Model() string {
	return c.model
}

func (c *AnthropicClient) Complete(ctx context.Context, prompt, system string) (string, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: defaultMaxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	}
	if system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}

	started := time.Now()
	response, err := c.messages.New(ctx, params)
	elapsedMS := time.Since(started).Milliseconds()
	if err != nil {
		anthropicLogger.Warn("LLM call failed", "model", c.model, "elapsed_ms", elapsedMS, "error", err)
		return "", fmt.Errorf("%w: anthropic: %v", ErrRequest, err)
	}

	anthropicLogger.Info("LLM call",
		"model", c.model,
		"input_tokens", response.Usage.InputTokens,
		"output_tokens", response.Usage.OutputTokens,
		"elapsed_ms", elapsedMS,
	)

	var text strings.Builder
	for _, block := range response.Content {
		if b, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(b.Text)
		}
	}
	return text.String(), nil
}
