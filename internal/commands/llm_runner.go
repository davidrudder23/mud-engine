package commands

// LLMCommandRunner is how a typed line becomes a command: it
// dispatches the line to one command and returns the text to show the
// player.
//
// It tries to match the line itself first (matchLocally: exact alias
// prefix, then fuzzy within 1 and 2 edits), running the command
// straight away without an LLM call. Only a line nothing matches
// locally goes to the LLM -- e.g. "head north please".
//
// For that, each command already declares its Name, Describe text,
// Aliases, and Parameters. This runner turns that into a system prompt,
// asks the LLM which command (if any) the line invokes and what values
// to use for its parameters, then hands off to that command's own
// RunCommand.
//
// Deliberately *not* a rewrite of each command's argument parsing: the
// model's extracted parameters become a normalized line via BuildLine
// (e.g. "go north"), handed to the matched command's RunCommand as if
// typed -- so `pick up`'s substring matching, `go`'s "Go where?"
// prompt, etc. all keep working unmodified. A missing required
// parameter isn't validated here at all; the command already prompts
// for it.
//
// Failure modes (a request error, malformed/unparseable JSON, a
// hallucinated command name, or "command": null) all fall back to the
// "Unknown command" reply -- from the player's side, a line the model
// couldn't place looks the same as one no verb recognizes.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"mudengine/internal/llm"
	"mudengine/internal/logging"
)

var llmRunnerLogger = logging.For("mud.commands.llm_runner")

const instructions = `You are the command parser for a text-based multi-user dungeon (MUD) game.
A player just typed a line of text. Decide which ONE of the commands listed
below it invokes, and extract that command's parameter values from the text.

Commands:
%s

Respond with ONLY a JSON object (no markdown fences, no other text), in
exactly this shape:
{"command": "<one of the command names above, or null if none apply>", "parameters": {"<name>": "<value>", ...}}

Only include a parameter in "parameters" if its value is actually present
in the player's text -- never invent or guess a value. If a required
parameter is missing, still return the best-matching command name; the
game will ask the player for it. If no command plausibly applies, set
"command" to null.`

var codeFence = regexp.MustCompile("(?s)^```(?:json)?\\s*(.*?)\\s*```$")

func describeParameters(c Command) string {
	params := c.Parameters()
	if len(params) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(params))
	for _, p := range params {
		required := "optional"
		if p.Required {
			required = "required"
		}
		parts = append(parts, fmt.Sprintf("%s (%s, %s)", p.Name, p.Type, required))
	}
	return strings.Join(parts, ", ")
}

// CollapseWhitespace squeezes runs of whitespace to single spaces.
// Describe text is column-padded for the `help` listing (e.g. "go <dir>
// moves..."); this keeps it from looking broken in a prompt or an MCP
// tool description.
func CollapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// BuildSystemPrompt describes every command (name, description,
// parameters) and the JSON reply shape expected back.
func BuildSystemPrompt(commands []Command) string {
	lines := make([]string, 0, len(commands))
	for _, c := range commands {
		line := fmt.Sprintf("- %q: %s Parameters: %s.",
			c.Name(), CollapseWhitespace(c.Describe()), describeParameters(c))
		if aliases := c.Aliases()[1:]; len(aliases) > 0 {
			line += " Also typed as: " + strings.Join(aliases, ", ") + "."
		}
		lines = append(lines, line)
	}
	llmRunnerLogger.Debug("Built system prompt", "commands", strings.Join(lines, "\n"))
	return fmt.Sprintf(instructions, strings.Join(lines, "\n"))
}

// extractJSONObject is a best-effort parse of the model's reply into an
// object, stripping a markdown code fence if present (models sometimes
// wrap JSON in one despite being told not to). Nil if it's not
// parseable JSON, or parses to something other than an object.
func extractJSONObject(reply string) map[string]any {
	text := strings.TrimSpace(reply)
	if m := codeFence.FindStringSubmatch(text); m != nil {
		text = m[1]
	}
	var data any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return nil
	}
	object, _ := data.(map[string]any)
	return object
}

// parameterValues converts the model's parameter values to strings,
// dropping anything empty/null/false (the same values BuildLine would
// omit) or not a plain scalar.
func parameterValues(raw map[string]any) map[string]string {
	values := map[string]string{}
	for name, v := range raw {
		switch v := v.(type) {
		case string:
			values[name] = v
		case float64:
			if v != 0 {
				values[name] = strconv.FormatFloat(v, 'f', -1, 64)
			}
		case bool:
			if v {
				values[name] = "true"
			}
		}
	}
	return values
}

type LLMCommandRunner struct {
	client       llm.Client
	commands     []Command
	byName       map[string]Command
	systemPrompt string
}

func NewLLMCommandRunner(client llm.Client, commands []Command) *LLMCommandRunner {
	byName := make(map[string]Command, len(commands))
	for _, c := range commands {
		byName[c.Name()] = c
	}
	return &LLMCommandRunner{client: client, commands: commands, byName: byName, systemPrompt: BuildSystemPrompt(commands)}
}

func (r *LLMCommandRunner) Run(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	if command, matched, ok := matchLocally(line, r.commands); ok {
		return command.RunCommand(ctx, matched, sessionID)
	}

	reply, err := r.client.Complete(ctx, line, r.systemPrompt)
	if errors.Is(err, llm.ErrRequest) {
		llmRunnerLogger.Warn("LLM request failed", "line", line, "error", err)
		return UnknownCommand(line), nil
	}
	if err != nil {
		return "", err
	}

	data := extractJSONObject(reply)
	if data == nil {
		llmRunnerLogger.Warn("Could not parse LLM reply", "line", line, "reply", reply)
		return UnknownCommand(line), nil
	}

	name, _ := data["command"].(string)
	rawParameters, _ := data["parameters"].(map[string]any)

	if name == "" {
		// A well-formed, deliberate "no command applies" -- not a
		// parsing failure, so this doesn't warrant a warning.
		llmRunnerLogger.Info("LLM found no matching command", "line", line)
		return UnknownCommand(line), nil
	}

	command, ok := r.byName[name]
	if !ok {
		llmRunnerLogger.Info("LLM matched line to unregistered command", "line", line, "command", name)
		return UnknownCommand(line), nil
	}

	parameters := parameterValues(rawParameters)
	llmRunnerLogger.Debug("LLM matched command", "line", line, "command", name, "parameters", parameters)
	return command.RunCommand(ctx, BuildLine(command, parameters), sessionID)
}
