package telnet

// End-to-end tests for connection handling: login, the
// read/dispatch/respond loop, and cleanup on disconnect. These drive
// HandleClient with a fake connection instead of a real socket, and a
// FakePlayerRepository instead of Postgres -- only the transport and
// storage are faked; the rest is the real code path.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"mudengine/internal/commands"
	"mudengine/internal/llm"
	"mudengine/internal/llm/llmtest"
	"mudengine/internal/logging/logtest"
	"mudengine/internal/repository/repotest"
	"mudengine/internal/session"
	"mudengine/internal/world"
	"mudengine/internal/world/worldtest"
)

const telnetLog = "mud.telnet"

// fakeConn reads pre-scripted input, then behaves like a closed
// connection (EOF) -- or returns readErr instead, if set -- and
// collects everything written to it.
type fakeConn struct {
	in      io.Reader
	readErr error
	out     bytes.Buffer
	closed  bool
}

func newConn(lines ...string) *fakeConn {
	var in strings.Builder
	for _, line := range lines {
		in.WriteString(line + "\r\n")
	}
	return &fakeConn{in: strings.NewReader(in.String())}
}

func (c *fakeConn) Read(p []byte) (int, error) {
	n, err := c.in.Read(p)
	if errors.Is(err, io.EOF) && c.readErr != nil {
		return n, c.readErr
	}
	return n, err
}

func (c *fakeConn) Write(p []byte) (int, error) {
	if c.closed {
		return 0, errors.New("write after close")
	}
	return c.out.Write(p)
}

func (c *fakeConn) Close() error {
	c.closed = true
	return nil
}

type fixture struct {
	world  *world.World
	repo   *repotest.FakePlayerRepository
	server *Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	w := worldtest.TwoRooms()
	repo := repotest.New()
	sessions := session.NewRegistry()
	// Every line the tests type matches a command locally, except
	// "dance", which this fake LLM declines to place.
	matcher := &llmtest.FakeClient{Reply: `{"command": null, "parameters": {}}`}
	runner := commands.NewLLMCommandRunner(matcher, commands.BuildAll(sessions, w, repo, nil, nil))
	return &fixture{
		world:  w,
		repo:   repo,
		server: &Server{World: w, Players: repo, Sessions: sessions, Runner: runner, Lock: &sync.Mutex{}},
	}
}

func (f *fixture) runClient(conn *fakeConn) string {
	f.server.HandleClient(context.Background(), conn, "127.0.0.1:12345")
	return conn.out.String()
}

func (f *fixture) description(id string) string {
	return worldtest.MustRoom(f.world, id).Description
}

func (f *fixture) savedRoom(t *testing.T, name string) string {
	t.Helper()
	player, err := f.repo.GetByName(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return player.CurrentRoomID
}

func (f *fixture) assertNobodyPlaced(t *testing.T) {
	t.Helper()
	if n := f.server.Sessions.Len(); n != 0 {
		t.Errorf("%d sessions left", n)
	}
	for _, room := range f.world.Rooms() {
		if len(room.Players) != 0 {
			t.Errorf("room %s still has players %v", room.ID, room.Players)
		}
	}
}

func TestFullSessionLoginLookGoQuit(t *testing.T) {
	f := newFixture(t)
	conn := newConn("alice", "look", "go north", "quit")
	transcript := f.runClient(conn)

	for _, want := range []string{
		"What is your name?", "Welcome, alice!",
		f.description(worldtest.StartID), f.description(worldtest.NorthID),
	} {
		if !strings.Contains(transcript, want) {
			t.Errorf("transcript missing %q", want)
		}
	}
	if !conn.closed { // quit must actually close the connection
		t.Error("connection not closed")
	}
	f.assertNobodyPlaced(t)
	if got := f.savedRoom(t, "alice"); got != worldtest.NorthID {
		t.Errorf("saved room = %q", got)
	}
}

func TestUnknownCommandGetsAReplyAndSessionContinues(t *testing.T) {
	f := newFixture(t)
	transcript := f.runClient(newConn("alice", "dance", "look", "quit"))
	if !strings.Contains(transcript, "Unknown command: 'dance'") || !strings.Contains(transcript, "You can try:") {
		t.Errorf("transcript = %q", transcript)
	}
}

func TestInvalidDirectionDoesNotMove(t *testing.T) {
	f := newFixture(t)
	transcript := f.runClient(newConn("alice", "go west", "quit"))
	if !strings.Contains(transcript, "You can't go 'west' from here.") || f.savedRoom(t, "alice") != worldtest.StartID {
		t.Errorf("transcript = %q", transcript)
	}
}

func TestBlankLinesAreIgnored(t *testing.T) {
	f := newFixture(t)
	logs := logtest.Capture(t)
	transcript := f.runClient(newConn("alice", "", "   ", "look", "quit"))

	if strings.Contains(transcript, "Unknown command") {
		t.Errorf("blank lines produced replies: %q", transcript)
	}
	// Only "look" and "quit" are real command lines.
	if n := len(slices.DeleteFunc(logs.Messages(telnetLog), func(m string) bool { return m != "Command" })); n != 2 {
		t.Errorf("%d Command records, want 2", n)
	}
}

func TestDisconnectBeforeGivingAName(t *testing.T) {
	f := newFixture(t)
	logs := logtest.Capture(t)
	transcript := f.runClient(newConn())

	if !strings.Contains(transcript, "What is your name?") {
		t.Errorf("transcript = %q", transcript)
	}
	f.assertNobodyPlaced(t)
	if players, _ := f.repo.ListAll(context.Background()); len(players) != 0 {
		t.Errorf("players created: %v", players)
	}
	messages := logs.Messages(telnetLog)
	if slices.Contains(messages, "Login") || slices.Contains(messages, "Logout") ||
		!slices.Contains(messages, "Connection opened") || !slices.Contains(messages, "Connection closed") {
		t.Errorf("messages = %v", messages)
	}
}

func TestDisconnectMidSessionCleansUp(t *testing.T) {
	f := newFixture(t)
	logs := logtest.Capture(t)
	f.runClient(newConn("alice", "look")) // no quit -- just EOF

	f.assertNobodyPlaced(t)
	if record, ok := logs.Find(telnetLog, "Logout"); !ok || record.Attrs["reason"] != "disconnect" {
		t.Errorf("logout record = %v", record)
	}
}

func TestBlankNameFallsBackToAdventurer(t *testing.T) {
	f := newFixture(t)
	if transcript := f.runClient(newConn("", "quit")); !strings.Contains(transcript, "Welcome, adventurer!") {
		t.Errorf("transcript = %q", transcript)
	}
}

func TestReadErrorIsHandledLikeADisconnect(t *testing.T) {
	f := newFixture(t)
	logs := logtest.Capture(t)
	conn := newConn("alice", "look")
	conn.readErr = errors.New("connection reset")

	f.runClient(conn)

	if !conn.closed {
		t.Error("connection not closed")
	}
	f.assertNobodyPlaced(t)
	record, ok := logs.Find(telnetLog, "Connection error")
	if !ok || record.Level != slog.LevelWarn || !strings.Contains(record.Attrs["error"], "connection reset") {
		t.Errorf("records = %v", logs.Records(telnetLog))
	}
	if record, _ := logs.Find(telnetLog, "Logout"); record.Attrs["reason"] != "error" {
		t.Errorf("logout reason = %q", record.Attrs["reason"])
	}
}

func TestOverlongLineIsHandledLikeADisconnect(t *testing.T) {
	f := newFixture(t)
	f.runClient(newConn("alice", strings.Repeat("x", maxLineBytes+1)))
	f.assertNobodyPlaced(t)
}

func TestTwoLoginsSameNameAreIndependent(t *testing.T) {
	// Neither has disconnected when the second logs in: two independent
	// *Players for the same row, both in the room, each movable alone.
	f := newFixture(t)
	ctx := context.Background()
	login := func() (*world.Player, *session.Session) {
		player, err := f.server.login(ctx, newLineReader(strings.NewReader("alice\r\n")), io.Discard)
		if err != nil || player == nil {
			t.Fatalf("login: %v, %v", player, err)
		}
		f.world.PlacePlayer(player)
		return player, f.server.Sessions.Create(player)
	}
	a, sessionA := login()
	b, _ := login()

	if a == b || a.ID != b.ID {
		t.Fatalf("a=%p b=%p ids %s/%s", a, b, a.ID, b.ID)
	}
	if n := len(worldtest.MustRoom(f.world, worldtest.StartID).Players); n != 2 {
		t.Fatalf("%d players in start", n)
	}

	f.server.Runner.Run(ctx, "go north", sessionA.ID)
	if a.CurrentRoomID != worldtest.NorthID || b.CurrentRoomID != worldtest.StartID ||
		!slices.Contains(worldtest.MustRoom(f.world, worldtest.StartID).Players, b) ||
		!slices.Contains(worldtest.MustRoom(f.world, worldtest.NorthID).Players, a) {
		t.Error("moving one moved the other")
	}
}

func TestLoginResetsPlayerWhoseSavedRoomNoLongerExists(t *testing.T) {
	f := newFixture(t)
	logs := logtest.Capture(t)
	f.repo.Create(context.Background(), "bob", "a-room-that-got-deleted")

	transcript := f.runClient(newConn("bob", "quit"))

	// Sent back to the deterministic default start room.
	defaultRoom := f.world.StartingRoomID()
	if !strings.Contains(transcript, "["+defaultRoom+"]") || f.savedRoom(t, "bob") != defaultRoom {
		t.Errorf("transcript = %q", transcript)
	}
	if record, ok := logs.Find(telnetLog, "Player's saved room no longer exists; reset"); !ok ||
		record.Level != slog.LevelWarn || record.Attrs["saved_room"] != "a-room-that-got-deleted" {
		t.Errorf("records = %v", logs.Records(telnetLog))
	}
}

func TestLoginCommandAndLogoutAreLogged(t *testing.T) {
	f := newFixture(t)
	f.repo.Create(context.Background(), "alice", worldtest.StartID)
	logs := logtest.Capture(t)

	f.runClient(newConn("alice", "look", "go north", "quit"))

	if login, ok := logs.Find(telnetLog, "Login"); !ok || login.Attrs["player"] != "alice" || login.Attrs["new_player"] != "false" {
		t.Errorf("login record = %v", login)
	}
	var lines []string
	for _, r := range logs.Records(telnetLog) {
		if r.Message == "Command" {
			lines = append(lines, r.Attrs["line"])
			if r.Attrs["player"] != "alice" || r.Attrs["session"] == "" {
				t.Errorf("command record = %v", r)
			}
		}
	}
	if !slices.Equal(lines, []string{"look", "go north", "quit"}) {
		t.Errorf("command lines = %v", lines)
	}
	if logout, ok := logs.Find(telnetLog, "Logout"); !ok || logout.Attrs["reason"] != "quit" || logout.Attrs["player"] != "alice" {
		t.Errorf("logout record = %v", logout)
	}
}

func TestNewPlayerIsFlaggedInLoginLog(t *testing.T) {
	f := newFixture(t)
	logs := logtest.Capture(t)
	f.runClient(newConn("carol", "quit"))
	if login, _ := logs.Find(telnetLog, "Login"); login.Attrs["new_player"] != "true" {
		t.Errorf("login record = %v", login)
	}
}

func TestBuildRunner(t *testing.T) {
	isolateLLMConfig := func(t *testing.T) {
		t.Setenv("MUD_LLM_CONFIG_FILE", filepath.Join(t.TempDir(), "llm.config.json"))
		for _, name := range []string{"MUD_LLM_PROVIDER", "MUD_LLM_API_KEY", "MUD_LLM_MODEL"} {
			t.Setenv(name, "")
		}
	}
	build := func(t *testing.T) (*commands.LLMCommandRunner, error) {
		f := newFixture(t)
		return BuildRunner(f.server.Sessions, f.world, f.repo, nil)
	}

	t.Run("builds with an api key", func(t *testing.T) {
		isolateLLMConfig(t)
		t.Setenv("MUD_LLM_API_KEY", "sk-test")
		if runner, err := build(t); err != nil || runner == nil {
			t.Errorf("got %v, %v", runner, err)
		}
	})
	t.Run("fails at startup without an api key", func(t *testing.T) {
		isolateLLMConfig(t)
		if _, err := build(t); !errors.Is(err, llm.ErrConfig) {
			t.Errorf("got %v, want ErrConfig", err)
		}
	})
}

// Ensures the test binary never picks up a real llm.config.json from
// the package directory.
func TestMain(m *testing.M) {
	os.Setenv("MUD_LLM_CONFIG_FILE", filepath.Join(os.TempDir(), "mud-engine-telnet-test-no-such-llm.config.json"))
	os.Exit(m.Run())
}

// pipeClient is one end of a live connection to HandleClient, for tests
// that need two players connected at once.
type pipeClient struct {
	t    *testing.T
	conn net.Conn
	done chan struct{}
}

func (f *fixture) connect(t *testing.T, name string) *pipeClient {
	t.Helper()
	client, server := net.Pipe()
	c := &pipeClient{t: t, conn: client, done: make(chan struct{})}
	go func() {
		defer close(c.done)
		f.server.HandleClient(context.Background(), server, name)
	}()
	t.Cleanup(func() {
		client.Close()
		<-c.done
	})
	c.readUntil("What is your name? ")
	c.send(name)
	c.readUntil("Welcome, " + name + "!")
	return c
}

func (c *pipeClient) send(line string) {
	c.t.Helper()
	if _, err := io.WriteString(c.conn, line+"\r\n"); err != nil {
		c.t.Fatal(err)
	}
}

// readUntil reads until the output so far contains want, failing the
// test if that takes more than a few seconds.
func (c *pipeClient) readUntil(want string) string {
	c.t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var out strings.Builder
	buf := make([]byte, 4096)
	for !strings.Contains(out.String(), want) {
		n, err := c.conn.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			c.t.Fatalf("waiting for %q: %v; got %q", want, err, out.String())
		}
	}
	return out.String()
}

func TestSayIsDeliveredToAnotherConnectionInTheRoom(t *testing.T) {
	f := newFixture(t)
	alice := f.connect(t, "alice")
	bob := f.connect(t, "bob")

	// A reader has to be draining each side of a net.Pipe, so bob
	// listens while alice speaks.
	heard := make(chan string, 1)
	go func() { heard <- bob.readUntil("alice says: hello bob\r\n") }()
	alice.send("say hello bob")
	alice.readUntil("You say: hello bob\r\n")
	<-heard

	// Not in the room any more, not in earshot.
	bob.send("go north")
	bob.readUntil(f.description(worldtest.NorthID))
	alice.send("say anyone?")
	alice.readUntil("You say: anyone?\r\n")
	bob.send("look")
	if out := bob.readUntil(f.description(worldtest.NorthID)); strings.Contains(out, "anyone?") {
		t.Errorf("bob heard alice from another room: %q", out)
	}
}
