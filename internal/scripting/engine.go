// Package scripting lets wizzards give NPCs and items behaviour in Lua,
// loaded, reloaded, and unloaded while the server runs. The design is
// in lua_scripting.md; what a script looks like is in lua_examples.md.
//
// An NPC or item names its script in JSON ("script": "rat.lua", a file
// in World.ScriptsDir). A script returns a class -- Monster:extend() or
// Item:extend() -- and overrides the hooks it cares about; the engine
// calls them (PlayerEntered, NPCAttacked, ItemHit, ...) from the
// commands where those things happen. Anything without a script, or
// whose script is missing, broken, or disabled, behaves exactly as it
// would with no scripting at all.
//
// Each NPC and item gets its own Lua table (its `self`) the first time
// a hook involves it, and keeps it for as long as it lives. Which class
// that table belongs to is looked up by script name on every access,
// so Reload only has to replace the class: every live instance picks up
// the new behaviour immediately and keeps its own state.
//
// Safety, for scripts written by trusted but fallible wizzards:
//
//   - Only Lua's base, table, string, and math libraries are open; no
//     files, no OS, no loading other code.
//   - Every call into Lua runs under a deadline (Options.HookTimeout).
//     Hooks run while the caller holds the game lock, so a runaway
//     script would otherwise freeze the whole MUD.
//   - A script that fails MaxFailures times in a row is disabled until
//     it's reloaded.
//   - Reload runs the new version through Admit first, which fires
//     simulated traffic at it and rejects one that errors, uses too
//     much memory, or keeps holding more of it.
//
// An Engine does no locking of its own: like the World it scripts,
// everything that touches it holds the game lock. A nil *Engine is
// valid and does nothing, so callers don't need to check for one.
package scripting

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"mudengine/internal/logging"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

var logger = logging.For("mud.scripting")

//go:embed stdlib.lua
var stdlib string

const (
	// DefaultHookTimeout is how long one call into Lua may run.
	DefaultHookTimeout = 250 * time.Millisecond
	// MaxFailures is how many times in a row a script's hooks may fail
	// before the script is disabled.
	MaxFailures = 5
	// MaxRoomNPCs is the most NPCs a room can hold before room:spawn
	// stops adding more.
	MaxRoomNPCs = 20
	// The longest string string.rep will build.
	maxRepBytes = 1 << 20
)

// Kind is what a script's class extends.
type Kind string

const (
	Monster Kind = "monster"
	Item    Kind = "item"
)

// Options customizes New. Every field is optional.
type Options struct {
	// How long one call into Lua may run (DefaultHookTimeout if zero).
	HookTimeout time.Duration
	// Persists a player a script changed (player:damage). Nil means
	// such changes last only as long as everything else in memory.
	SavePlayer func(ctx context.Context, player *world.Player) error
}

// class is one loaded script.
type class struct {
	table *lua.LTable
	kind  Kind
	// Consecutive failed hook calls; at MaxFailures the class is
	// disabled.
	failures int
	disabled bool
}

// binding ties an instance table (a script's `self`) to what it is.
type binding struct {
	// A *world.NPC or *world.Item.
	obj    any
	script string
}

type Engine struct {
	L        *lua.LState
	world    *world.World
	sessions *session.Registry
	timeout  time.Duration
	save     func(ctx context.Context, player *world.Player) error

	// Loaded scripts, by file name.
	classes map[string]*class
	// Instance tables, by the *world.NPC / *world.Item they belong to,
	// and the reverse.
	instances map[any]*lua.LTable
	bindings  map[*lua.LTable]*binding

	monsterBase, itemBase         *lua.LTable
	instanceMeta                  *lua.LTable
	playerMeta, roomMeta, statsMt *lua.LTable

	// Set only while a hook runs.
	running bool
	// The context the hook was called with (without the hook deadline).
	ctx context.Context
	// The player whose command triggered the hook, if any. Text for
	// them is collected in replies -- to go out as part of the
	// command's own reply, in order -- instead of being sent.
	actor   *world.Player
	replies []string
	// What Monster.on_attacked does: the NPC's own attack back.
	strike func()

	// The last hook failure, for Admit.
	lastErr error
	// A scratch engine (Admit's) doesn't log script failures or
	// disable anything; its caller reports them instead.
	scratch bool
}

// New builds an engine for w with nothing loaded yet (see LoadAll).
// sessions is how script output reaches players; nil means it reaches
// only the player whose command triggered it.
func New(w *world.World, sessions *session.Registry, opts Options) (*Engine, error) {
	e := &Engine{
		L:         lua.NewState(lua.Options{SkipOpenLibs: true}),
		world:     w,
		sessions:  sessions,
		timeout:   opts.HookTimeout,
		save:      opts.SavePlayer,
		classes:   map[string]*class{},
		instances: map[any]*lua.LTable{},
		bindings:  map[*lua.LTable]*binding{},
	}
	if e.timeout <= 0 {
		e.timeout = DefaultHookTimeout
	}
	if err := e.openSandbox(); err != nil {
		e.L.Close()
		return nil, err
	}
	return e, nil
}

// Close releases the Lua state. The engine must not be used afterwards.
func (e *Engine) Close() {
	if e != nil {
		e.L.Close()
	}
}

// openSandbox opens the safe libraries, removes what's left that could
// reach outside the game, and installs the base classes and the API.
func (e *Engine) openSandbox() error {
	L := e.L
	for _, lib := range []struct {
		name string
		open lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		if err := L.CallByParam(lua.P{Fn: L.NewFunction(lib.open), NRet: 0, Protect: true}, lua.LString(lib.name)); err != nil {
			return fmt.Errorf("scripting: opening Lua library %q: %w", lib.name, err)
		}
	}
	// The base library's ways of loading code, reaching the registry,
	// or escaping a script's own environment.
	for _, name := range []string{
		"dofile", "loadfile", "load", "loadstring", "require", "module",
		"collectgarbage", "newproxy", "getfenv", "setfenv",
	} {
		L.SetGlobal(name, lua.LNil)
	}
	// print goes to the log: stdout belongs to the MCP transport.
	L.SetGlobal("print", L.NewFunction(luaPrint))
	if stringLib, ok := L.GetGlobal("string").(*lua.LTable); ok {
		stringLib.RawSetString("rep", L.NewFunction(luaRep))
		stringLib.RawSetString("dump", lua.LNil)
	}

	fn, err := L.Load(strings.NewReader(stdlib), "stdlib.lua")
	if err != nil {
		return fmt.Errorf("scripting: %w", err)
	}
	if err := L.CallByParam(lua.P{Fn: fn, NRet: 0, Protect: true}); err != nil {
		return fmt.Errorf("scripting: %w", err)
	}
	var ok bool
	if e.monsterBase, ok = L.GetGlobal("Monster").(*lua.LTable); !ok {
		return errors.New("scripting: stdlib.lua didn't define Monster")
	}
	if e.itemBase, ok = L.GetGlobal("Item").(*lua.LTable); !ok {
		return errors.New("scripting: stdlib.lua didn't define Item")
	}
	e.installAPI()
	return nil
}

func luaPrint(L *lua.LState) int {
	parts := make([]string, 0, L.GetTop())
	for i := 1; i <= L.GetTop(); i++ {
		parts = append(parts, L.ToStringMeta(L.Get(i)).String())
	}
	logger.Info("Script print", "text", strings.Join(parts, "\t"))
	return 0
}

// luaRep is string.rep with a cap on the result: one call can't build
// a string the hook deadline would never get the chance to interrupt.
func luaRep(L *lua.LState) int {
	s := L.CheckString(1)
	n := L.CheckInt(2)
	if n <= 0 {
		L.Push(lua.LString(""))
		return 1
	}
	if len(s) > 0 && n > maxRepBytes/len(s) {
		L.RaiseError("string.rep: result would be longer than %d bytes", maxRepBytes)
	}
	L.Push(lua.LString(strings.Repeat(s, n)))
	return 1
}

// ScriptName turns what a wizzard typed ("swamp-dragon" or
// "swamp-dragon.lua") into a script file name, or an error if it isn't
// a plain name.
func ScriptName(arg string) (string, error) {
	name := strings.TrimSpace(arg)
	if name == "" {
		return "", errors.New("no script named")
	}
	if !strings.HasSuffix(name, ".lua") {
		name += ".lua"
	}
	if name != filepath.Base(name) || name == ".lua" || strings.ContainsAny(name, " \t") {
		return "", fmt.Errorf("%q isn't a script name", arg)
	}
	return name, nil
}

// pcall calls fn under the hook deadline, returning its first result
// if nret is 1.
func (e *Engine) pcall(ctx context.Context, fn lua.LValue, nret int, args ...lua.LValue) (lua.LValue, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	e.L.SetContext(ctx)
	defer e.L.RemoveContext()

	top := e.L.GetTop()
	if err := e.L.CallByParam(lua.P{Fn: fn, NRet: nret, Protect: true}, args...); err != nil {
		e.L.SetTop(top)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("ran for longer than %v and was stopped", e.timeout)
		}
		return nil, scriptError(err)
	}
	var result lua.LValue = lua.LNil
	if nret == 1 {
		result = e.L.Get(-1)
		e.L.Pop(1)
	}
	return result, nil
}

// scriptError strips the Go-side stack trace off a Lua error, leaving
// the message a wizzard can act on ("rat.lua:7: attempt to ...").
func scriptError(err error) error {
	var apiErr *lua.ApiError
	if errors.As(err, &apiErr) && apiErr.Object != nil && apiErr.Object != lua.LNil {
		return errors.New(apiErr.Object.String())
	}
	return err
}

// compile runs a script's source and checks that what it returns is a
// class extending Monster or Item.
func (e *Engine) compile(ctx context.Context, name, source string) (*class, error) {
	fn, err := e.L.Load(strings.NewReader(source), name)
	if err != nil {
		return nil, scriptError(err)
	}
	// Each script gets its own environment that falls back to the real
	// globals, so a forgotten `local` can't leak into other scripts or
	// outlive a reload.
	env := e.L.NewTable()
	envMeta := e.L.NewTable()
	envMeta.RawSetString("__index", e.L.Get(lua.GlobalsIndex))
	e.L.SetMetatable(env, envMeta)
	e.L.SetFEnv(fn, env)

	result, err := e.pcall(ctx, fn, 1)
	if err != nil {
		return nil, err
	}
	table, ok := result.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("%s must end by returning its class (it returned %s)", name, result.Type())
	}
	kind := e.kindOf(table)
	if kind == "" || table == e.monsterBase || table == e.itemBase {
		return nil, fmt.Errorf("%s must return a class made with Monster:extend() or Item:extend()", name)
	}
	return &class{table: table, kind: kind}, nil
}

// kindOf walks table's ancestry looking for Monster or Item ("" if it
// has neither).
func (e *Engine) kindOf(table *lua.LTable) Kind {
	// Bounded, in case a script builds itself a metatable cycle.
	for depth := 0; table != nil && depth < 64; depth++ {
		switch table {
		case e.monsterBase:
			return Monster
		case e.itemBase:
			return Item
		}
		table, _ = e.L.GetMetatable(table).(*lua.LTable)
	}
	return ""
}

func (e *Engine) read(name string) (string, error) {
	if e.world.ScriptsDir == "" {
		return "", errors.New("this world has no scripts directory")
	}
	source, err := os.ReadFile(filepath.Join(e.world.ScriptsDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no such script: %s", name)
	}
	if err != nil {
		return "", err
	}
	return string(source), nil
}

// Reload loads name (a file name in the world's scripts directory, see
// ScriptName) from disk, replacing whatever version was running --
// whether or not there was one. The new version has to pass Admit
// first; if it doesn't, or doesn't compile, the error says why and the
// old version carries on untouched.
func (e *Engine) Reload(ctx context.Context, name string) (Kind, error) {
	if e == nil {
		return "", errors.New("scripting is not enabled")
	}
	source, err := e.read(name)
	if err != nil {
		return "", err
	}
	if err := Admit(ctx, name, source, e.world.NPCs); err != nil {
		return "", err
	}
	loaded, err := e.compile(ctx, name, source)
	if err != nil {
		return "", err
	}
	e.classes[name] = loaded
	logger.Info("Script loaded", "script", name, "kind", loaded.kind)
	return loaded.kind, nil
}

// Unload drops a script, reporting whether it was loaded. Whatever was
// using it goes back to the default behaviour, keeping its instance
// state in case the script is loaded again.
func (e *Engine) Unload(name string) bool {
	if e == nil {
		return false
	}
	if _, ok := e.classes[name]; !ok {
		return false
	}
	delete(e.classes, name)
	logger.Info("Script unloaded", "script", name)
	return true
}

// Loaded returns the names of the loaded scripts, sorted.
func (e *Engine) Loaded() []string {
	if e == nil {
		return nil
	}
	names := make([]string, 0, len(e.classes))
	for name := range e.classes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LoadAll loads every script the world refers to: each NPC definition's,
// and each item's wherever it starts out. A script that's missing or
// fails to load is logged and skipped -- its owner just behaves as if
// it had none -- so one bad script can't stop the server starting.
// Returns how many loaded and how many didn't.
func (e *Engine) LoadAll(ctx context.Context) (loaded, failed int) {
	if e == nil {
		return 0, 0
	}
	for _, name := range Referenced(e.world) {
		if _, err := e.Reload(ctx, name); err != nil {
			failed++
			logger.Error("Script failed to load", "script", name, "error", err)
			continue
		}
		loaded++
	}
	return loaded, failed
}

// Referenced returns the names of every script w's NPCs and items
// refer to, sorted.
func Referenced(w *world.World) []string {
	seen := map[string]bool{}
	items := func(items []*world.Item) {
		for _, item := range items {
			seen[item.Script] = true
		}
	}
	for _, npc := range w.NPCs {
		seen[npc.Script] = true
		items(npc.Inventory)
	}
	for _, room := range w.Rooms() {
		items(room.Items)
		for _, npc := range room.NPCs {
			seen[npc.Script] = true
			items(npc.Inventory)
		}
	}
	delete(seen, "")
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// active returns script's class if it's loaded and not disabled.
func (e *Engine) active(script string) *class {
	if script == "" {
		return nil
	}
	if loaded := e.classes[script]; loaded != nil && !loaded.disabled {
		return loaded
	}
	return nil
}

// base is the base class for kind.
func (e *Engine) base(kind Kind) *lua.LTable {
	if kind == Item {
		return e.itemBase
	}
	return e.monsterBase
}

// instance returns obj's instance table -- what its script sees as
// `self` -- creating it the first time.
func (e *Engine) instance(obj any, script string) *lua.LTable {
	if table, ok := e.instances[obj]; ok {
		return table
	}
	table := e.L.NewTable()
	e.L.SetMetatable(table, e.instanceMeta)
	e.instances[obj] = table
	e.bindings[table] = &binding{obj: obj, script: script}
	return table
}

// forget drops obj's instance table, and with it whatever state its
// script kept there.
func (e *Engine) forget(obj any) {
	if table, ok := e.instances[obj]; ok {
		delete(e.bindings, table)
		delete(e.instances, obj)
	}
}
