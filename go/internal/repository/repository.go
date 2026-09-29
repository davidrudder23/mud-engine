// Package repository is the interface for player persistence.
//
// Game logic should depend on PlayerRepository, not on pgx (or
// Postgres at all) directly. That's what lets the storage backend
// change later -- a different database, a cache in front of it,
// sharding by player id -- without touching anything that uses one.
package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"mudengine/internal/world"
)

var (
	// ErrDuplicatePlayerName is returned by Create when a player name
	// is already taken.
	ErrDuplicatePlayerName = errors.New("player name already taken")
	// ErrNotFound is returned by GetByID/GetByName when no such player
	// exists.
	ErrNotFound = errors.New("player not found")
)

type PlayerRepository interface {
	// Create creates and persists a new player. Returns an error
	// wrapping ErrDuplicatePlayerName if name is already taken.
	Create(ctx context.Context, name, startingRoomID string) (*world.Player, error)
	GetByID(ctx context.Context, id uuid.UUID) (*world.Player, error)
	GetByName(ctx context.Context, name string) (*world.Player, error)
	// Save persists changes to an existing (already-created) player.
	Save(ctx context.Context, player *world.Player) error
	ListAll(ctx context.Context) ([]*world.Player, error)
}
