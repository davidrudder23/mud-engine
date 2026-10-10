package commands

import (
	"context"

	"github.com/google/uuid"

	"mudengine/internal/scripting"
	"mudengine/internal/session"
)

// wizzardOnly is what the script commands share: who's asking, and
// whether they're allowed to.
type wizzardOnly struct {
	sessions *session.Registry
	scripts  *scripting.Engine
	// Whether the player with this name may run the command.
	isWizzard func(name string) bool
}

// script resolves the script a wizzard named. refusal, if non-empty,
// is what to tell the player instead of running the command.
func (w wizzardOnly) script(sessionID uuid.UUID, argument, verb string) (name, refusal string, err error) {
	s, err := w.sessions.Get(sessionID)
	if err != nil {
		return "", "", err
	}
	if !w.isWizzard(s.Player.Name) {
		return "", "Only a wizzard can do that.", nil
	}
	if argument == "" {
		return "", verb + " which script?", nil
	}
	name, err = scripting.ScriptName(argument)
	if err != nil {
		return "", quoted(argument) + " isn't a script name.", nil
	}
	return name, "", nil
}

// ReloadCommand loads a Lua script from the world's scripts directory
// into the running game, replacing the version that was running if
// there was one. Everything already using the script picks up the new
// behaviour at once and keeps its state.
//
// The new version is checked first (scripting.Admit); if it fails
// that, or doesn't compile, the wizzard is told why and the old
// version keeps running.
//
// Wizzards only (config.Wizzards).
type ReloadCommand struct {
	alwaysInRoom
	verbs
	wizzardOnly
}

func NewReloadCommand(sessions *session.Registry, scripts *scripting.Engine, isWizzard func(name string) bool) *ReloadCommand {
	return &ReloadCommand{verbs: verbs{"reload"}, wizzardOnly: wizzardOnly{sessions, scripts, isWizzard}}
}

func (c *ReloadCommand) Describe() string {
	return "reload <script>  loads a script's latest version (wizzards only)"
}
func (c *ReloadCommand) Parameters() []Parameter {
	return []Parameter{{Name: "script", Type: String, Required: true}}
}

func (c *ReloadCommand) RunCommand(ctx context.Context, line string, sessionID uuid.UUID) (string, error) {
	name, refusal, err := c.script(sessionID, c.argument(line), "Reload")
	if err != nil || refusal != "" {
		return refusal, err
	}
	kind, err := c.scripts.Reload(ctx, name)
	if err != nil {
		// The wizzard's problem to fix, not a server failure.
		return "Not loaded: " + err.Error(), nil
	}
	article := "a"
	if kind == scripting.Item {
		article = "an"
	}
	return "Loaded " + name + " (" + article + " " + string(kind) + ").", nil
}

// UnloadCommand drops a script from the running game: whatever was
// using it goes back to behaving as if it had none, until it's
// reloaded. Wizzards only.
type UnloadCommand struct {
	alwaysInRoom
	verbs
	wizzardOnly
}

func NewUnloadCommand(sessions *session.Registry, scripts *scripting.Engine, isWizzard func(name string) bool) *UnloadCommand {
	return &UnloadCommand{verbs: verbs{"unload"}, wizzardOnly: wizzardOnly{sessions, scripts, isWizzard}}
}

func (c *UnloadCommand) Describe() string {
	return "unload <script>  stops a script running (wizzards only)"
}
func (c *UnloadCommand) Parameters() []Parameter {
	return []Parameter{{Name: "script", Type: String, Required: true}}
}

func (c *UnloadCommand) RunCommand(_ context.Context, line string, sessionID uuid.UUID) (string, error) {
	name, refusal, err := c.script(sessionID, c.argument(line), "Unload")
	if err != nil || refusal != "" {
		return refusal, err
	}
	if !c.scripts.Unload(name) {
		return name + " isn't loaded.", nil
	}
	return "Unloaded " + name + ".", nil
}
