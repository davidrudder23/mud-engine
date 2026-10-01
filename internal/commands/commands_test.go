package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"mudengine/internal/llm"
	"mudengine/internal/llm/llmtest"
	"mudengine/internal/repository/repotest"
	"mudengine/internal/session"
	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

// fixture is a two-room world with one player, alice, logged in at
// worldtest.StartID.
type fixture struct {
	world    *world.World
	sessions *session.Registry
	repo     *repotest.FakePlayerRepository
	player   *world.Player
	session  *session.Session
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{world: worldtest.TwoRooms(), sessions: session.NewRegistry(), repo: repotest.New()}
	player, err := f.repo.Create(context.Background(), "alice", worldtest.StartID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.world.PlacePlayer(player); err != nil {
		t.Fatal(err)
	}
	f.player = player
	f.session = f.sessions.Create(player)
	return f
}

func (f *fixture) room(id string) *world.Room {
	return worldtest.MustRoom(f.world, id)
}

func (f *fixture) run(t *testing.T, c Command, line string) string {
	t.Helper()
	out, err := c.RunCommand(context.Background(), line, f.session.ID)
	if err != nil {
		t.Fatalf("RunCommand(%q): %v", line, err)
	}
	return out
}

func TestMatchesVerb(t *testing.T) {
	tests := []struct {
		line, verb string
		want       bool
	}{
		{"look", "look", true},
		{"go north", "go", true},
		{"LOOK around", "look", true},
		{"Go North", "go", true},
		{"quit", "go", false},
		// Documented, intentional quirk: a plain prefix check, so
		// "goblin" matches verb "go".
		{"goblin", "go", true},
	}
	for _, tt := range tests {
		if got := MatchesVerb(tt.line, tt.verb); got != tt.want {
			t.Errorf("MatchesVerb(%q, %q) = %v", tt.line, tt.verb, got)
		}
	}
}

func TestCommandArgument(t *testing.T) {
	tests := []struct{ line, verb, want string }{
		{"pick up sword", "pick up", "sword"},
		// Splitting on the first space would cut after "pick", not
		// after "pick up".
		{"pick up a rusty sword", "pick up", "a rusty sword"},
		{"pick up", "pick up", ""},
		{"pick up   ", "pick up", ""},
		{"PICK UP sword", "pick up", "sword"},
	}
	for _, tt := range tests {
		if got := CommandArgument(tt.line, tt.verb); got != tt.want {
			t.Errorf("CommandArgument(%q, %q) = %q, want %q", tt.line, tt.verb, got, tt.want)
		}
	}
}

func TestMatchAliasPrefersTheLongestMatch(t *testing.T) {
	aliases := []string{"pick", "pick up", "take"}
	tests := []struct {
		line, want string
		ok         bool
	}{
		{"pick up sword", "pick up", true},
		{"pick sword", "pick", true},
		{"TAKE sword", "take", true},
		{"drop sword", "", false},
	}
	for _, tt := range tests {
		got, ok := MatchAlias(tt.line, aliases)
		if got != tt.want || ok != tt.ok {
			t.Errorf("MatchAlias(%q) = %q, %v; want %q, %v", tt.line, got, ok, tt.want, tt.ok)
		}
	}
	if got := (verbs{"pick", "pick up"}).argument("pick up a rusty sword"); got != "a rusty sword" {
		t.Errorf("argument = %q", got)
	}
}

func TestAliases(t *testing.T) {
	f := newFixture(t)
	for _, c := range BuildAll(f.sessions, f.world, f.repo, nil) {
		if aliases := c.Aliases(); len(aliases) == 0 || aliases[0] != c.Name() {
			t.Errorf("%s: Aliases() = %v, want Name() first", c.Name(), aliases)
		}
	}
	goCmd := NewGoCommand(f.sessions, f.world, f.repo)
	if got := goCmd.Aliases(); !slices.Equal(got, []string{"go", "leave", "exit", "travel"}) {
		t.Errorf("go aliases = %v", got)
	}
}

func TestGoAnswersToEveryAlias(t *testing.T) {
	for _, alias := range []string{"go", "leave", "exit", "travel", "Travel"} {
		t.Run(alias, func(t *testing.T) {
			f := newFixture(t)
			goCmd := NewGoCommand(f.sessions, f.world, f.repo)
			line := alias + " north"
			if !goCmd.MatchCommand(line) {
				t.Fatalf("doesn't match %q", line)
			}
			if out := f.run(t, goCmd, line); out != f.room(worldtest.NorthID).Format() {
				t.Errorf("%q: %q", line, out)
			}
			if out := f.run(t, goCmd, alias); out != "Go where?" {
				t.Errorf("%q alone: %q", alias, out)
			}
		})
	}
}

func TestBuildLine(t *testing.T) {
	f := newFixture(t)
	look := NewLookCommand(f.sessions, f.world)
	goCmd := NewGoCommand(f.sessions, f.world, f.repo)
	pickUp := NewPickUpCommand(f.sessions, f.world)

	tests := []struct {
		command Command
		params  map[string]string
		want    string
	}{
		{look, nil, "look"},
		{goCmd, map[string]string{"direction": "north"}, "go north"},
		{pickUp, map[string]string{"item": "rusty sword"}, "pick up rusty sword"},
		{goCmd, nil, "go"}, // missing parameter is just omitted
		{goCmd, map[string]string{"direction": "north", "speed": "fast"}, "go north"}, // undeclared ignored
	}
	for _, tt := range tests {
		if got := BuildLine(tt.command, tt.params); got != tt.want {
			t.Errorf("BuildLine(%s, %v) = %q, want %q", tt.command.Name(), tt.params, got, tt.want)
		}
	}
}

func TestBuildLineRoundTripsThroughRunCommand(t *testing.T) {
	f := newFixture(t)
	goCmd := NewGoCommand(f.sessions, f.world, f.repo)
	out := f.run(t, goCmd, BuildLine(goCmd, map[string]string{"direction": "north"}))
	if f.player.CurrentRoomID != worldtest.NorthID || out != f.room(worldtest.NorthID).Format() {
		t.Errorf("room=%q out=%q", f.player.CurrentRoomID, out)
	}
}

func TestParameters(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		command Command
		want    []Parameter
	}{
		{NewLookCommand(f.sessions, f.world), nil},
		{NewQuitCommand(f.sessions), nil},
		{NewHelpCommand(f.sessions, f.world, &[]Command{}), nil},
		{NewGoCommand(f.sessions, f.world, f.repo), []Parameter{{"direction", String, true}}},
		{NewPickUpCommand(f.sessions, f.world), []Parameter{{"item", String, true}}},
		{NewDropCommand(f.sessions, f.world), []Parameter{{"item", String, true}}},
		{NewOrderCommand(), []Parameter{{"drink", String, true}}},
		{NewTalkCommand(f.sessions, f.world, nil), []Parameter{{"npc", String, true}}},
	}
	for _, tt := range tests {
		if got := tt.command.Parameters(); !slices.Equal(got, tt.want) {
			t.Errorf("%s: Parameters() = %v, want %v", tt.command.Name(), got, tt.want)
		}
	}
}

func TestMatchCommand(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		command Command
		yes, no []string
	}{
		{NewLookCommand(f.sessions, f.world), []string{"look", "look around"}, []string{"go north"}},
		{NewHelpCommand(f.sessions, f.world, &[]Command{}), []string{"help"}, []string{"look"}},
		{NewQuitCommand(f.sessions), []string{"quit"}, []string{"look"}},
		{NewPickUpCommand(f.sessions, f.world), []string{"pick up sword"}, []string{"look", "pick"}},
		{NewDropCommand(f.sessions, f.world), []string{"drop sword"}, []string{"look"}},
		{NewOrderCommand(), []string{"order drink"}, []string{"go north"}},
		{NewTalkCommand(f.sessions, f.world, nil), []string{"talk old tom", "talk to old tom"}, []string{"look"}},
	}
	for _, tt := range tests {
		for _, line := range tt.yes {
			if !tt.command.MatchCommand(line) {
				t.Errorf("%s should match %q", tt.command.Name(), line)
			}
		}
		for _, line := range tt.no {
			if tt.command.MatchCommand(line) {
				t.Errorf("%s should not match %q", tt.command.Name(), line)
			}
		}
	}
}

func TestLook(t *testing.T) {
	f := newFixture(t)
	look := NewLookCommand(f.sessions, f.world)

	out := f.run(t, look, "look")
	start := f.room(worldtest.StartID)
	if want := start.Format() + "\r\nYou can try: " + strings.Join(start.GetCommands(), ", "); out != want {
		t.Errorf("got %q, want %q", out, want)
	}
	if !strings.Contains(out, "You can try: look, go") {
		t.Errorf("missing hint: %q", out)
	}

	f.world.MovePlayer(f.player, worldtest.NorthID)
	if out := f.run(t, look, "look"); out != LookText(f.room(worldtest.NorthID)) {
		t.Errorf("after move: %q", out)
	}
}

func TestGo(t *testing.T) {
	f := newFixture(t)
	goCmd := NewGoCommand(f.sessions, f.world, f.repo)

	if out := f.run(t, goCmd, "go"); out != "Go where?" {
		t.Errorf("no direction: %q", out)
	}
	if out := f.run(t, goCmd, "go west"); out != "You can't go 'west' from here." {
		t.Errorf("unknown direction: %q", out)
	}
	// Doorway direction is "north"; "North" must not match.
	if out := f.run(t, goCmd, "go North"); out != "You can't go 'North' from here." {
		t.Errorf("case-sensitive: %q", out)
	}
	if f.player.CurrentRoomID != worldtest.StartID {
		t.Fatal("player moved on a failed go")
	}

	out := f.run(t, goCmd, "go north")
	if f.player.CurrentRoomID != worldtest.NorthID || out != f.room(worldtest.NorthID).Format() ||
		!slices.Contains(f.room(worldtest.NorthID).Players, f.player) ||
		slices.Contains(f.room(worldtest.StartID).Players, f.player) {
		t.Errorf("move not applied: room=%q out=%q", f.player.CurrentRoomID, out)
	}
	// The move must survive a reconnect, i.e. it was actually saved.
	persisted, _ := f.repo.GetByID(context.Background(), f.player.ID)
	if persisted.CurrentRoomID != worldtest.NorthID {
		t.Errorf("persisted room = %q", persisted.CurrentRoomID)
	}

	f.run(t, goCmd, "go south")
	if f.player.CurrentRoomID != worldtest.StartID || !slices.Contains(f.room(worldtest.StartID).Players, f.player) {
		t.Error("round trip failed")
	}
}

func TestHelp(t *testing.T) {
	f := newFixture(t)
	look := NewLookCommand(f.sessions, f.world)
	goCmd := NewGoCommand(f.sessions, f.world, f.repo)
	quit := NewQuitCommand(f.sessions)
	list := []Command{look, goCmd, quit}
	help := NewHelpCommand(f.sessions, f.world, &list)

	want := strings.Join([]string{look.Describe(), goCmd.Describe(), quit.Describe()}, "\r\n")
	if out := f.run(t, help, "help"); out != want {
		t.Errorf("got %q, want %q", out, want)
	}
	// "look"/"go" from Room.GetCommands are covered by registered
	// commands, so must not be listed a second time.
	lines := strings.Split(f.run(t, help, "help"), "\r\n")
	if slices.Contains(lines, "look") || slices.Contains(lines, "go") {
		t.Errorf("duplicated verbs: %q", lines)
	}

	for _, c := range append(list, help) {
		if !c.MatchRoom(f.room(worldtest.StartID)) {
			t.Errorf("%s.MatchRoom should be the always-true stub", c.Name())
		}
	}
}

func TestHelpListsItselfWhenInItsOwnList(t *testing.T) {
	f := newFixture(t)
	list := []Command{NewLookCommand(f.sessions, f.world)}
	help := NewHelpCommand(f.sessions, f.world, &list)
	list = append(list, help)

	if lines := strings.Split(f.run(t, help, "help"), "\r\n"); !slices.Contains(lines, help.Describe()) {
		t.Errorf("help not listed: %q", lines)
	}
}

func TestHelpListsRoomVerbsWithNoRegisteredCommand(t *testing.T) {
	f := newFixture(t)
	list := []Command{NewLookCommand(f.sessions, f.world)}
	help := NewHelpCommand(f.sessions, f.world, &list)
	start := f.room(worldtest.StartID)
	start.Items = append(start.Items, &world.Item{ID: "rock", Name: "a rock"})
	start.Commands = append(start.Commands, "ring bell")

	lines := strings.Split(f.run(t, help, "help"), "\r\n")
	if !slices.Contains(lines, "pick up") || !slices.Contains(lines, "ring bell") {
		t.Errorf("lines = %q", lines)
	}
}

func TestQuit(t *testing.T) {
	f := newFixture(t)
	if out := f.run(t, NewQuitCommand(f.sessions), "quit"); out != "" || !f.session.ShouldClose {
		t.Errorf("out=%q ShouldClose=%v", out, f.session.ShouldClose)
	}
}

func TestPickUp(t *testing.T) {
	f := newFixture(t)
	pickUp := NewPickUpCommand(f.sessions, f.world)
	start := f.room(worldtest.StartID)

	if out := f.run(t, pickUp, "pick up"); out != "Pick up what?" {
		t.Errorf("no item: %q", out)
	}
	if out := f.run(t, pickUp, "pick up sword"); out != "You don't see 'sword' here." {
		t.Errorf("absent: %q", out)
	}

	for _, line := range []string{"pick up rock", "pick up SHINY"} { // by id; by name substring, any case
		rock := &world.Item{ID: "rock", Name: "A Shiny Rock"}
		start.Items = []*world.Item{rock}
		f.player.Inventory = nil

		if out := f.run(t, pickUp, line); out != "You pick up A Shiny Rock." {
			t.Errorf("%q: %q", line, out)
		}
		if slices.Contains(start.Items, rock) || !slices.Contains(f.player.Inventory, rock) {
			t.Errorf("%q: item not moved to inventory", line)
		}
	}
}

func TestDrop(t *testing.T) {
	f := newFixture(t)
	drop := NewDropCommand(f.sessions, f.world)
	start := f.room(worldtest.StartID)

	if out := f.run(t, drop, "drop"); out != "Drop what?" {
		t.Errorf("no item: %q", out)
	}
	if out := f.run(t, drop, "drop sword"); out != "You aren't carrying 'sword'." {
		t.Errorf("absent: %q", out)
	}

	for _, line := range []string{"drop rock", "drop SHINY"} {
		rock := &world.Item{ID: "rock", Name: "A Shiny Rock"}
		f.player.Inventory = []*world.Item{rock}
		start.Items = nil

		if out := f.run(t, drop, line); out != "You drop A Shiny Rock." {
			t.Errorf("%q: %q", line, out)
		}
		if slices.Contains(f.player.Inventory, rock) || !slices.Contains(start.Items, rock) {
			t.Errorf("%q: item not moved to room", line)
		}
	}
}

func TestDropLandsInTheCurrentRoom(t *testing.T) {
	f := newFixture(t)
	rock := &world.Item{ID: "rock", Name: "A Shiny Rock"}
	f.player.Inventory = []*world.Item{rock}
	f.world.MovePlayer(f.player, worldtest.NorthID)

	f.run(t, NewDropCommand(f.sessions, f.world), "drop rock")

	if slices.Contains(f.room(worldtest.StartID).Items, rock) || !slices.Contains(f.room(worldtest.NorthID).Items, rock) {
		t.Error("item didn't land in the player's current room")
	}
}

func TestPickUpThenDropRoundTrip(t *testing.T) {
	f := newFixture(t)
	start := f.room(worldtest.StartID)
	rock := &world.Item{ID: "rock", Name: "A Shiny Rock"}
	start.Items = []*world.Item{rock}

	f.run(t, NewPickUpCommand(f.sessions, f.world), "pick up rock")
	f.run(t, NewDropCommand(f.sessions, f.world), "drop rock")

	if !slices.Contains(start.Items, rock) || slices.Contains(f.player.Inventory, rock) {
		t.Error("round trip failed")
	}
}

func TestOrder(t *testing.T) {
	f := newFixture(t)
	order := NewOrderCommand()
	if out := f.run(t, order, "order"); out != "Order what?" {
		t.Errorf("no drink: %q", out)
	}
	if out := f.run(t, order, "order drink"); !strings.Contains(out, "drink") {
		t.Errorf("got %q", out)
	}
}

func talkFixture(t *testing.T) (*fixture, *llmtest.FakeClient, *TalkCommand) {
	f := newFixture(t)
	fake := &llmtest.FakeClient{Reply: "Welcome, traveler."}
	talk := NewTalkCommand(f.sessions, f.world, func() (llm.Client, error) { return fake, nil })
	return f, fake, talk
}

func (f *fixture) addTom(description string) {
	start := f.room(worldtest.StartID)
	start.NPCs = append(start.NPCs, &world.NPC{ID: "old-tom", Name: "Old Tom", Description: description})
}

func TestTalk(t *testing.T) {
	f, fake, talk := talkFixture(t)

	if out := f.run(t, talk, "talk"); out != "Talk to whom?" {
		t.Errorf("no npc: %q", out)
	}
	if out := f.run(t, talk, "talk old tom"); out != "You don't see 'old tom' here." {
		t.Errorf("absent: %q", out)
	}

	f.addTom("A grizzled bartender.")
	for _, line := range []string{"talk old-tom", "talk to TOM"} { // by id; by name, "to" prefix, any case
		if out := f.run(t, talk, line); out != "Old Tom says: Welcome, traveler." {
			t.Errorf("%q: %q", line, out)
		}
	}

	system := fake.Calls[0].System
	if !strings.Contains(system, "Old Tom") || !strings.Contains(system, "A grizzled bartender.") {
		t.Errorf("system prompt = %q", system)
	}
}

func TestTalkBuildsLLMClientLazilyAndOnce(t *testing.T) {
	f := newFixture(t)
	f.addTom("")
	builds := 0
	talk := NewTalkCommand(f.sessions, f.world, func() (llm.Client, error) {
		builds++
		return &llmtest.FakeClient{Reply: "Hi."}, nil
	})
	if builds != 0 {
		t.Fatal("client built before first use")
	}
	f.run(t, talk, "talk tom")
	f.run(t, talk, "talk tom")
	if builds != 1 {
		t.Errorf("built %d times, want 1", builds)
	}
}

func TestTalkRequestErrorFallsBackGracefully(t *testing.T) {
	f := newFixture(t)
	f.addTom("")
	fake := &llmtest.FakeClient{Err: fmt.Errorf("%w: down", llm.ErrRequest)}
	talk := NewTalkCommand(f.sessions, f.world, func() (llm.Client, error) { return fake, nil })

	if out := f.run(t, talk, "talk tom"); out != "Old Tom doesn't seem to hear you." {
		t.Errorf("got %q", out)
	}
}

func TestTalkConfigErrorIsRetriedNextCall(t *testing.T) {
	f := newFixture(t)
	f.addTom("")
	attempts := 0
	talk := NewTalkCommand(f.sessions, f.world, func() (llm.Client, error) {
		attempts++
		if attempts == 1 {
			return nil, fmt.Errorf("%w: no api key", llm.ErrConfig)
		}
		return &llmtest.FakeClient{Reply: "Welcome, traveler."}, nil
	})

	if out := f.run(t, talk, "talk tom"); out != "Old Tom doesn't seem to hear you." {
		t.Errorf("first: %q", out)
	}
	if out := f.run(t, talk, "talk tom"); out != "Old Tom says: Welcome, traveler." {
		t.Errorf("second: %q", out)
	}
}

func TestTalkUnexpectedErrorIsReturned(t *testing.T) {
	f := newFixture(t)
	f.addTom("")
	boom := errors.New("boom")
	talk := NewTalkCommand(f.sessions, f.world, func() (llm.Client, error) { return nil, boom })

	if _, err := talk.RunCommand(context.Background(), "talk tom", f.session.ID); !errors.Is(err, boom) {
		t.Errorf("got %v, want boom", err)
	}
}

// runnerFixture is a runner over every command whose LLM answers "no
// command" to anything that reaches it -- so a line only runs a command
// if local (exact/fuzzy) matching places it. The returned fake records
// whether the LLM was asked. talk gets its own fake, for its greetings.
func runnerFixture(t *testing.T) (*fixture, *LLMCommandRunner, *llmtest.FakeClient) {
	f := newFixture(t)
	talkLLM := &llmtest.FakeClient{Reply: "Welcome, traveler."}
	all := BuildAll(f.sessions, f.world, f.repo, func() (llm.Client, error) { return talkLLM, nil })
	matcher := &llmtest.FakeClient{Reply: `{"command": null, "parameters": {}}`}
	return f, NewLLMCommandRunner(matcher, all), matcher
}

func (f *fixture) runLine(t *testing.T, r *LLMCommandRunner, line string) string {
	t.Helper()
	out, err := r.Run(context.Background(), line, f.session.ID)
	if err != nil {
		t.Fatalf("Run(%q): %v", line, err)
	}
	return out
}

func TestRunnerDispatchesLocallyMatchedLines(t *testing.T) {
	f, runner, matcher := runnerFixture(t)
	start := f.room(worldtest.StartID)
	start.Items = append(start.Items, &world.Item{ID: "rock", Name: "a rock"})
	f.addTom("")

	tests := []struct{ line, want string }{
		{"look", LookText(start)}, // first, while the room is as set up above
		{"pick up rock", "You pick up a rock."},
		{"drop rock", "You drop a rock."},
		{"talk to old tom", "Old Tom says: Welcome, traveler."},
	}
	for _, tt := range tests {
		if out := f.runLine(t, runner, tt.line); out != tt.want {
			t.Errorf("%q: got %q, want %q", tt.line, out, tt.want)
		}
	}
	if out := f.runLine(t, runner, "order drink"); !strings.Contains(out, "drink") {
		t.Errorf("order: %q", out)
	}
	if len(matcher.Calls) != 0 {
		t.Errorf("LLM asked about locally matched lines: %v", matcher.Calls)
	}
}

func TestRunnerAsksTheLLMOnlyWhenNothingMatchesLocally(t *testing.T) {
	f, runner, matcher := runnerFixture(t)

	for _, line := range []string{"dance", "  dance  "} {
		if out := f.runLine(t, runner, line); out != "Unknown command: 'dance'" {
			t.Errorf("%q: got %q", line, out)
		}
	}
	if len(matcher.Calls) != 2 || matcher.Calls[0].Prompt != "dance" {
		t.Errorf("LLM calls = %v, want one per unmatched line", matcher.Calls)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"go", "go", 0},
		{"", "look", 4},
		{"lok", "look", 1},   // insertion
		{"loook", "look", 1}, // deletion
		{"lxok", "look", 1},  // substitution
		{"og", "go", 2},      // transposition costs two
		{"kitten", "sitting", 3},
		{"café", "cafe", 1}, // runes, not bytes
	}
	for _, tt := range tests {
		if got := Levenshtein(tt.a, tt.b); got != tt.want {
			t.Errorf("Levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestAllowedDistance(t *testing.T) {
	tests := map[string]int{
		"n": 0, "go": 0, "say": 0, // 1-3 letters: exact only
		"look": 1, "quit": 1, // 4 letters
		"leave": 2, "travel": 2, "pick up": 2, // 5+ letters (spaces don't count)
		"a b c": 0, // 3 letters across three words
	}
	for alias, want := range tests {
		if got := AllowedDistance(alias); got != want {
			t.Errorf("AllowedDistance(%q) = %d, want %d", alias, got, want)
		}
	}
}

func TestFuzzyMatchAlias(t *testing.T) {
	tests := []struct {
		line     string
		aliases  []string
		distance int
		want     string
		ok       bool
	}{
		{"lok", []string{"look"}, 1, "look", true},
		{"LOK around", []string{"look"}, 1, "look around", true},
		{"lave  north", []string{"go", "leave"}, 1, "leave north", true},
		{"lve north", []string{"leave"}, 1, "", false}, // 2 edits, but this pass allows 1
		{"lve north", []string{"leave"}, 2, "leave north", true},
		// A multi-word alias is compared against as many leading words.
		{"pcik up a rusty sword", []string{"pick up"}, 2, "pick up a rusty sword", true},
		{"pick", []string{"pick up"}, 2, "", false}, // too few words to compare
		{"xyzzy", []string{"look"}, 2, "", false},
		// The alias's own AllowedDistance caps any pass.
		{"og north", []string{"go"}, 2, "", false},    // 2 letters: exact only
		{"lk around", []string{"look"}, 2, "", false}, // 4 letters: 1 edit at most
	}
	for _, tt := range tests {
		got, ok := FuzzyMatchAlias(tt.line, tt.aliases, tt.distance)
		if got != tt.want || ok != tt.ok {
			t.Errorf("FuzzyMatchAlias(%q, %v, %d) = %q, %v; want %q, %v",
				tt.line, tt.aliases, tt.distance, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRunnerFuzzyMatching(t *testing.T) {
	f, runner, _ := runnerFixture(t)
	start := f.room(worldtest.StartID)
	start.Items = append(start.Items, &world.Item{ID: "rock", Name: "a rock"})

	// Lines expected to be Unknown reach the fake LLM, which answers
	// "no command" -- so they prove local matching rejected them.
	tests := []struct{ line, want string }{
		{"lok", LookText(start)},                // look, 1 edit
		{"looi", LookText(start)},               // look, 1 edit
		{"pcik up rock", "You pick up a rock."}, // pick up, 2 edits
		{"drp rock", "You drop a rock."},        // drop, 1 edit
		{"ordr ale", "You order ale. It's set down in front of you without much ceremony."},
		{"xyzzy", "Unknown command: 'xyzzy'"},
		{"dance", "Unknown command: 'dance'"},
		// Short aliases get little or no slack.
		{"dorp rock", "Unknown command: 'dorp rock'"}, // 2 edits from 4-letter "drop"
		{"og north", "Unknown command: 'og north'"},   // 3-or-fewer-letter "go": exact only
		{"n", "Unknown command: 'n'"},
		{"hi", "Unknown command: 'hi'"},
		{"no", "Unknown command: 'no'"},
		{"ok", "Unknown command: 'ok'"},               // 2 edits from "look"
		{"hello", "Unknown command: 'hello'"},         // 2 edits from "help"
		{"take rock", "Unknown command: 'take rock'"}, // 2 edits from "talk"
	}
	for _, tt := range tests {
		if out := f.runLine(t, runner, tt.line); out != tt.want {
			t.Errorf("%q: got %q, want %q", tt.line, out, tt.want)
		}
	}

	// go's 5+-letter aliases still get 2 edits.
	if out := f.runLine(t, runner, "trvael north"); out != f.room(worldtest.NorthID).Format() {
		t.Errorf("trvael north: %q", out)
	}
}

// fakeCommand records the line it was run with.
type fakeCommand struct {
	alwaysInRoom
	verbs
	ran *string
}

func (c fakeCommand) Describe() string        { return "" }
func (c fakeCommand) Parameters() []Parameter { return nil }
func (c fakeCommand) RunCommand(_ context.Context, line string, _ uuid.UUID) (string, error) {
	*c.ran = line
	return c.Name(), nil
}

func TestRunnerPassOrder(t *testing.T) {
	var ran string
	// 5 letters each, so both get the full 2 edits.
	first := fakeCommand{verbs: verbs{"abcde"}, ran: &ran}
	second := fakeCommand{verbs: verbs{"abcdf"}, ran: &ran}
	runner := NewLLMCommandRunner(&llmtest.FakeClient{Err: errors.New("LLM should not be asked")}, []Command{first, second})
	run := func(line string) string {
		out, err := runner.Run(context.Background(), line, uuid.Nil)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	// "abxdf" is 2 edits from the first command but 1 from the second:
	// the closer match wins even though it's later in the list.
	if out := run("abxdf now"); out != "abcdf" || ran != "abcdf now" {
		t.Errorf("got %q (ran %q), want abcdf", out, ran)
	}
	// Tied distance: list order decides.
	if out := run("abcdx"); out != "abcde" {
		t.Errorf("got %q, want abcde", out)
	}
	// An exact (prefix) match beats any fuzzy one.
	if out := run("abcdfg"); out != "abcdf" || ran != "abcdfg" {
		t.Errorf("got %q (ran %q), want abcdf unchanged", out, ran)
	}
}

func TestRunnerHelpListsEveryCommand(t *testing.T) {
	f, runner, _ := runnerFixture(t)
	out := f.runLine(t, runner, "help")
	for _, want := range []string{"look", "go <dir>", "quit", "help", "pick up <item>", "drop <item>", "order <drink>", "talk to <npc>"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q: %q", want, out)
		}
	}
}
