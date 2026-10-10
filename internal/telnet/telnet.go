// Package telnet is the telnet server.
//
// On connect: ask for a name, get-or-create the matching Player (via
// PlayerRepository), place it in the world, and start a Session tied to
// that Player. From then on, every line the client sends is handed to
// the command runner, and whatever it returns is written back. Anything
// sent to the session in the meantime (Session.Send -- another player's
// `say`) is written as it arrives, between replies.
//
// Logging in twice under the same name (two connections) is allowed on
// purpose: each gets its own Session and its own *Player for the same
// underlying row -- so the same-named player can appear twice in a room
// at once, moving independently. There's no real auth: whatever name
// you type, you get (or become) that player.
//
// Lines are dispatched by commands.LLMCommandRunner: exact and fuzzy
// alias matching first, then an LLM for anything those don't place. It
// needs a configured LLM client -- BuildRunner fails immediately at
// startup if one isn't set up, rather than on the first line someone
// types.
//
// Concurrency: each connection and each wandering NPC runs in its own
// goroutine, and they all share the world. Server.Lock is the one game
// lock around every world access -- placing/removing a player, running
// a command, moving an NPC, ticking scripts -- so the game logic
// itself, Lua included, stays single-threaded. Commands run under it end to end, including any LLM
// call or DB save they make, which serializes those across players;
// fine at this scale, and the obvious thing to revisit if it isn't.
package telnet

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"mudengine/internal/combat"
	"mudengine/internal/commands"
	"mudengine/internal/llm"
	"mudengine/internal/logging"
	"mudengine/internal/npcpatrol"
	"mudengine/internal/repository"
	"mudengine/internal/scripting"
	"mudengine/internal/session"
	"mudengine/internal/world"
)

const (
	DefaultHost = "127.0.0.1"
	DefaultPort = 8023
	// Longest line a client may send before being disconnected.
	maxLineBytes = 64 * 1024
)

var logger = logging.For("mud.telnet")

// BuildRunner builds every command (commands.BuildAll) and the runner
// that dispatches lines to them, using the configured LLM client.
// scripts is the Lua engine the commands call hooks on (nil for none).
func BuildRunner(sessions *session.Registry, w *world.World, players repository.PlayerRepository, scripts *scripting.Engine) (*commands.LLMCommandRunner, error) {
	client, err := llm.GetClient()
	if err != nil {
		return nil, err
	}
	return commands.NewLLMCommandRunner(client, commands.BuildAll(sessions, w, players, nil, scripts)), nil
}

// StartScripts builds the Lua engine for w and loads every script the
// world refers to. A script that fails to load is logged and left out;
// only failing to start the engine itself is an error.
//
// Shared with the MCP server, like ResolvePlayer.
func StartScripts(ctx context.Context, w *world.World, sessions *session.Registry, players repository.PlayerRepository) (*scripting.Engine, error) {
	scripts, err := scripting.New(w, sessions, scripting.Options{SavePlayer: players.Save})
	if err != nil {
		return nil, err
	}
	loaded, failed := scripts.LoadAll(ctx)
	logger.Info("Loaded scripts", "loaded", loaded, "failed", failed, "dir", w.ScriptsDir)
	return scripts, nil
}

// ResolvePlayer gets the Player named name, creating one (starting in
// the default room) if none exists yet. Also resets CurrentRoomID (and
// persists that) if it points at a room the current world no longer
// has -- e.g. the map was redone since this player last saved.
//
// Shared with the MCP server, which gets its player's name from config
// instead of prompting, so both get identical get-or-create and
// stale-room handling.
func ResolvePlayer(ctx context.Context, w *world.World, players repository.PlayerRepository, name string) (*world.Player, error) {
	player, err := players.GetByName(ctx, name)
	isNew := errors.Is(err, repository.ErrNotFound)
	if isNew {
		player, err = players.Create(ctx, name, w.StartingRoomID())
		if errors.Is(err, repository.ErrDuplicatePlayerName) {
			// Someone else's login just created it between our check
			// and our insert -- fetch that row instead of failing.
			logger.Info("Duplicate-name race on create; using the existing row", "player", name)
			player, err = players.GetByName(ctx, name)
		}
	}
	if err != nil {
		return nil, err
	}

	if !w.HasRoom(player.CurrentRoomID) {
		// The world changed since this player last saved and their room
		// no longer exists. Send them back to the default start rather
		// than failing the login.
		stale := player.CurrentRoomID
		player.CurrentRoomID = w.StartingRoomID()
		if err := players.Save(ctx, player); err != nil {
			return nil, err
		}
		logger.Warn("Player's saved room no longer exists; reset",
			"player", name, "saved_room", stale, "room", player.CurrentRoomID)
	}

	logger.Info("Login", "player", name, "new_player", isNew, "room", player.CurrentRoomID)
	return player, nil
}

// Server holds everything connections share.
type Server struct {
	World    *world.World
	Players  repository.PlayerRepository
	Sessions *session.Registry
	Runner   *commands.LLMCommandRunner
	// The game lock -- see the package doc.
	Lock *sync.Mutex
}

// lineReader reads CR/LF-terminated lines, capped at maxLineBytes.
type lineReader struct {
	scanner *bufio.Scanner
}

func newLineReader(r io.Reader) *lineReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), maxLineBytes)
	return &lineReader{scanner: scanner}
}

// readLine returns the next line with surrounding whitespace trimmed
// and invalid UTF-8 replaced, or io.EOF once the client has
// disconnected.
func (r *lineReader) readLine() (string, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return strings.TrimSpace(strings.ToValidUTF8(r.scanner.Text(), "�")), nil
}

// login prompts for a name and returns the matching (or newly created)
// Player, or nil if the connection dropped before answering.
func (s *Server) login(ctx context.Context, lines *lineReader, conn io.Writer) (*world.Player, error) {
	if _, err := io.WriteString(conn, "What is your name? "); err != nil {
		return nil, err
	}
	name, err := lines.readLine()
	if errors.Is(err, io.EOF) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = "adventurer"
	}
	return ResolvePlayer(ctx, s.World, s.Players, name)
}

// HandleClient runs one connection from login to disconnect, and
// always closes conn.
func (s *Server) HandleClient(ctx context.Context, conn io.ReadWriteCloser, peer string) {
	logger.Info("Connection opened", "peer", peer)
	defer func() {
		conn.Close()
		logger.Info("Connection closed", "peer", peer)
	}()

	var sess *session.Session
	reason := "disconnect" // client EOF, the common case
	defer func() {
		if sess == nil {
			return
		}
		s.Lock.Lock()
		s.World.RemovePlayer(sess.Player)
		room := sess.Player.CurrentRoomID
		s.Lock.Unlock()
		s.Sessions.Remove(sess.ID)
		logger.Info("Logout", "session", sess.ID, "player", sess.Player.Name, "reason", reason, "room", room)
	}()

	err := s.serve(ctx, conn, &sess, &reason)
	if err != nil {
		reason = "error"
		logger.Warn("Connection error", "peer", peer, "error", err)
	}
}

// serve is HandleClient's login + command loop. It sets *sess once the
// player is placed (so HandleClient can clean up) and *reason to "quit"
// if the player quits.
func (s *Server) serve(ctx context.Context, conn io.ReadWriter, sess **session.Session, reason *string) error {
	lines := newLineReader(conn)

	player, err := s.login(ctx, lines, conn)
	if err != nil || player == nil {
		return err // nil player: disconnected before giving a name
	}

	s.Lock.Lock()
	err = s.World.PlacePlayer(player)
	var welcome string
	if err == nil {
		*sess = s.Sessions.Create(player)
		var room *world.Room
		if room, err = s.World.GetRoom(player.CurrentRoomID); err == nil {
			welcome = fmt.Sprintf("Welcome, %s!\r\n\r\n%s\r\n", player.Name, room.Format())
		}
	}
	s.Lock.Unlock()
	if err != nil {
		return err
	}
	logger.Info("Session started", "session", (*sess).ID, "player", player.Name, "room", player.CurrentRoomID)

	// From here on two goroutines write to conn: this one (replies to
	// what the player typed) and the one below (what other players
	// send them, e.g. `say`), so every write goes through write.
	var writeMu sync.Mutex
	write := func(text string) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err := io.WriteString(conn, text)
		return err
	}
	done := make(chan struct{})
	var pusher sync.WaitGroup
	pusher.Go(func() {
		for {
			select {
			case message := <-(*sess).Messages():
				// A failed write means the connection is going away;
				// the read loop notices that on its own.
				write(message + "\r\n")
			case <-done:
				return
			}
		}
	})
	defer func() {
		close(done)
		pusher.Wait()
	}()

	if err := write(welcome); err != nil {
		return err
	}

	for {
		line, err := lines.readLine()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if line == "" {
			continue
		}

		logger.Info("Command", "session", (*sess).ID, "player", player.Name, "line", line)
		s.Lock.Lock()
		response, err := s.Runner.Run(ctx, line, (*sess).ID)
		s.Lock.Unlock()
		if err != nil {
			return err
		}
		if err := write(response + "\r\n"); err != nil {
			return err
		}

		if (*sess).ShouldClose {
			*reason = "quit"
			return nil
		}
	}
}

// startWanderingNPCs starts one npcpatrol.RunForever goroutine per
// wandering NPC currently placed in the world, all stopped by ctx.
func startWanderingNPCs(ctx context.Context, w *world.World, lock sync.Locker, wg *sync.WaitGroup) {
	count := 0
	for _, room := range w.Rooms() {
		for _, npc := range room.NPCs {
			if !npc.IsWandering() {
				continue
			}
			count++
			wg.Go(func() { npcpatrol.RunForever(ctx, w, npc, lock) })
		}
	}
	if count > 0 {
		logger.Info("Started wandering NPC goroutines", "count", count)
	}
}

// Run serves telnet on host:port until ctx is cancelled.
func Run(ctx context.Context, w *world.World, players repository.PlayerRepository, host string, port int) error {
	sessions := session.NewRegistry()
	scripts, err := StartScripts(ctx, w, sessions, players)
	if err != nil {
		return err
	}
	defer scripts.Close()
	runner, err := BuildRunner(sessions, w, players, scripts)
	if err != nil {
		return err
	}
	server := &Server{World: w, Players: players, Sessions: sessions, Runner: runner, Lock: &sync.Mutex{}}

	listener, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return err
	}
	logger.Info("MUD telnet server listening", "addr", listener.Addr().String())

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	startWanderingNPCs(ctx, w, server.Lock, &wg)
	wg.Go(func() { combat.RunRegen(ctx, w, server.Lock) })
	wg.Go(func() { scripts.Run(ctx, server.Lock) })

	// Closing the listener is what unblocks Accept on shutdown; open
	// connections are closed too, so their read loops end.
	var (
		connsMu sync.Mutex
		conns   = map[net.Conn]bool{}
	)
	go func() {
		<-ctx.Done()
		listener.Close()
		connsMu.Lock()
		for conn := range conns {
			conn.Close()
		}
		connsMu.Unlock()
	}()

	var acceptErr error
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() == nil {
				acceptErr = err
				cancel()
			}
			break
		}
		connsMu.Lock()
		conns[conn] = true
		connsMu.Unlock()
		wg.Go(func() {
			server.HandleClient(ctx, conn, conn.RemoteAddr().String())
			connsMu.Lock()
			delete(conns, conn)
			connsMu.Unlock()
		})
	}

	wg.Wait()
	return acceptErr
}
