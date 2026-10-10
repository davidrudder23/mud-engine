// Package session tracks one connection's state.
//
// A session is tied to a Player, not to a room directly -- location
// lives on the player (Player.CurrentRoomID), since it's the player's
// location that persists, not the connection's. This is one-directional:
// a session knows its player, but a Player doesn't know which
// session(s) it's in -- the same player name can be behind multiple
// sessions at once, each with its own *Player.
package session

import (
	"fmt"
	"sync"

	"github.com/google/uuid"

	"mudengine/internal/world"
)

type Session struct {
	ID     uuid.UUID
	Player *world.Player
	// Set by the quit command; the telnet server's read loop checks
	// this after every command and closes the connection if it's set.
	ShouldClose bool
	// Text waiting to be shown to this connection that it didn't ask
	// for -- another player's `say`, say. See Send and Messages.
	outbox chan string
}

// How many undelivered messages a session holds before Send starts
// dropping them.
const outboxSize = 64

// Send queues text for this session's connection to show, outside the
// reply to whatever the player last typed. Never blocks: if the
// connection has stopped draining its outbox and it's full, the message
// is dropped and Send reports false.
func (s *Session) Send(text string) bool {
	select {
	case s.outbox <- text:
		return true
	default:
		return false
	}
}

// Messages is where whatever owns the connection receives what Send
// queued.
func (s *Session) Messages() <-chan string {
	return s.outbox
}

// Registry tracks live sessions, keyed by session id. Safe for
// concurrent use.
type Registry struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*Session
}

func NewRegistry() *Registry {
	return &Registry{sessions: map[uuid.UUID]*Session{}}
}

func (r *Registry) Create(player *world.Player) *Session {
	s := &Session{ID: uuid.New(), Player: player, outbox: make(chan string, outboxSize)}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.ID] = s
	return s
}

func (r *Registry) Get(id uuid.UUID) (*Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, fmt.Errorf("no such session: %s", id)
	}
	return s, nil
}

func (r *Registry) Remove(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
}

// InRoom returns every live session whose player stands in roomID, in
// no particular order.
func (r *Registry) InRoom(roomID string) []*Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found []*Session
	for _, s := range r.sessions {
		if s.Player.CurrentRoomID == roomID {
			found = append(found, s)
		}
	}
	return found
}

// Len is the number of live sessions.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}
