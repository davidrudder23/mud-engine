// Package repotest provides an in-memory PlayerRepository for tests.
package repotest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"mudengine/internal/repository"
	"mudengine/internal/world"
)

// FakePlayerRepository stands in for the Postgres repository so command
// and server tests don't need a real database. It deliberately mirrors
// the real one's behavior rather than the simplest thing that would
// pass: every GetByID/GetByName/Create call builds a *fresh* *Player
// from stored data (like a fresh row fetch would) instead of handing
// back a cached pointer. That matters because two logins under the
// same name must be two independent instances -- a fake that reused
// pointers would let a bug in that area pass unnoticed.
type FakePlayerRepository struct {
	mu    sync.Mutex
	rows  map[uuid.UUID]*row
	order []uuid.UUID
}

type row struct {
	name          string
	currentRoomID string
	stats         world.Stats
	createdAt     time.Time
	updatedAt     time.Time
}

var _ repository.PlayerRepository = (*FakePlayerRepository)(nil)

func New() *FakePlayerRepository {
	return &FakePlayerRepository{rows: map[uuid.UUID]*row{}}
}

func (f *FakePlayerRepository) Create(_ context.Context, name, startingRoomID string) (*world.Player, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.name == name {
			return nil, fmt.Errorf("%w: %q", repository.ErrDuplicatePlayerName, name)
		}
	}
	now := time.Now().UTC()
	id := uuid.New()
	f.rows[id] = &row{
		name: name, currentRoomID: startingRoomID, stats: world.DefaultPlayerStats(),
		createdAt: now, updatedAt: now,
	}
	f.order = append(f.order, id)
	return f.toPlayer(id), nil
}

func (f *FakePlayerRepository) GetByID(_ context.Context, id uuid.UUID) (*world.Player, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[id]; !ok {
		return nil, repository.ErrNotFound
	}
	return f.toPlayer(id), nil
}

func (f *FakePlayerRepository) GetByName(_ context.Context, name string) (*world.Player, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range f.order {
		if f.rows[id].name == name {
			return f.toPlayer(id), nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *FakePlayerRepository) Save(_ context.Context, player *world.Player) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[player.ID]
	if !ok {
		return fmt.Errorf("cannot save unknown player: %s", player.ID)
	}
	r.currentRoomID = player.CurrentRoomID
	if player.Stats != nil {
		r.stats = *player.Stats
	}
	r.updatedAt = time.Now().UTC()
	return nil
}

func (f *FakePlayerRepository) ListAll(_ context.Context) ([]*world.Player, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	players := make([]*world.Player, 0, len(f.order))
	for _, id := range f.order {
		players = append(players, f.toPlayer(id))
	}
	return players, nil
}

func (f *FakePlayerRepository) toPlayer(id uuid.UUID) *world.Player {
	r := f.rows[id]
	stats := r.stats // a fresh copy, like a fresh row fetch
	return &world.Player{
		ID:            id,
		Name:          r.name,
		CurrentRoomID: r.currentRoomID,
		CreatedAt:     r.createdAt,
		UpdatedAt:     r.updatedAt,
		Stats:         &stats,
	}
}
