package repository

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"mudengine/internal/db"
	"mudengine/internal/world"
)

// Integration tests against a real Postgres -- what the fake repository
// can't catch: SQL, column order, and the schema itself. They only run
// when MUD_TEST_DB_NAME names a scratch database to use (connection
// otherwise per the usual MUD_DB_* settings), e.g.
//
//	createdb mud_test && MUD_TEST_DB_NAME=mud_test go test ./internal/repository/
//
// They apply sql/schema.sql (harmless to re-run) and delete only the
// rows they create.
func newTestRepo(t *testing.T) *PostgresPlayerRepository {
	t.Helper()
	name := os.Getenv("MUD_TEST_DB_NAME")
	if name == "" {
		t.Skip("set MUD_TEST_DB_NAME to a scratch database to run Postgres tests")
	}
	t.Setenv("MUD_DB_NAME", name)

	ctx := context.Background()
	pool, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	schema, err := os.ReadFile("../../sql/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("applying schema: %v", err)
	}
	return NewPostgresPlayerRepository(pool)
}

// create makes a uniquely named player and deletes it after the test.
func create(t *testing.T, repo *PostgresPlayerRepository, room string) *world.Player {
	t.Helper()
	ctx := context.Background()
	player, err := repo.Create(ctx, "test-"+uuid.NewString(), room)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.pool.Exec(ctx, "DELETE FROM players WHERE id = $1", pgUUID(player.ID)) })
	return player
}

func TestPostgresCreateStartsWithDefaultStats(t *testing.T) {
	repo := newTestRepo(t)
	player := create(t, repo, "start")

	// The schema's column defaults must match DefaultPlayerStats.
	if player.Stats == nil || *player.Stats != world.DefaultPlayerStats() {
		t.Errorf("stats = %+v, want %+v", player.Stats, world.DefaultPlayerStats())
	}
	if player.ID == uuid.Nil || player.CurrentRoomID != "start" || player.CreatedAt.IsZero() {
		t.Errorf("player = %+v", player)
	}
}

func TestPostgresSaveRoundTripsRoomAndStats(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	player := create(t, repo, "start")

	player.CurrentRoomID = "north"
	// Every field distinct, so a swapped column can't go unnoticed.
	player.Stats = &world.Stats{
		Strength: 1, Dexterity: 2, Luck: 3, HitPoints: 4, MaxHitPoints: 5, Stamina: 6, MaxStamina: 7,
	}
	if err := repo.Save(ctx, player); err != nil {
		t.Fatal(err)
	}

	for name, get := range map[string]func() (*world.Player, error){
		"by id":   func() (*world.Player, error) { return repo.GetByID(ctx, player.ID) },
		"by name": func() (*world.Player, error) { return repo.GetByName(ctx, player.Name) },
	} {
		got, err := get()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.CurrentRoomID != "north" || *got.Stats != *player.Stats {
			t.Errorf("%s: room %q, stats %+v; want north, %+v", name, got.CurrentRoomID, got.Stats, player.Stats)
		}
		if got == player {
			t.Errorf("%s: returned the same instance, not a fresh row", name)
		}
	}

	all, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range all {
		if p.ID == player.ID {
			found = *p.Stats == *player.Stats
		}
	}
	if !found {
		t.Error("ListAll doesn't return the saved player with its stats")
	}
}

func TestPostgresErrors(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	player := create(t, repo, "start")

	if _, err := repo.Create(ctx, player.Name, "start"); !errors.Is(err, ErrDuplicatePlayerName) {
		t.Errorf("duplicate name: got %v", err)
	}
	if _, err := repo.GetByName(ctx, "test-no-such-"+uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown name: got %v", err)
	}
	if _, err := repo.GetByID(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: got %v", err)
	}
	if err := repo.Save(ctx, &world.Player{Name: "never-created"}); err == nil {
		t.Error("saving an uncreated player succeeded")
	}
}
