// Command demo-player exercises player creation/lookup/persistence
// against Postgres. Run migrate once first to create the schema.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"

	"mudengine/internal/db"
	"mudengine/internal/repository"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	pool, err := db.Open(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo := repository.NewPostgresPlayerRepository(pool)

	name := "demo-" + uuid.NewString()[:8]
	player, err := repo.Create(ctx, name, "room-1")
	if err != nil {
		return err
	}
	fmt.Printf("Created player: %+v\n", *player)

	fetched, err := repo.GetByID(ctx, player.ID)
	if err != nil {
		return err
	}
	fmt.Printf("Fetched by id:  %+v\n", *fetched)

	player.CurrentRoomID = "room-2"
	if err := repo.Save(ctx, player); err != nil {
		return err
	}
	moved, err := repo.GetByName(ctx, name)
	if err != nil {
		return err
	}
	fmt.Printf("After move:     %+v\n", *moved)

	_, err = repo.Create(ctx, name, "room-1")
	if errors.Is(err, repository.ErrDuplicatePlayerName) {
		fmt.Println("OK, got expected error:", err)
	} else {
		fmt.Println("ERROR: should have failed with ErrDuplicatePlayerName, got:", err)
	}

	all, err := repo.ListAll(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("\nAll players (%d):\n", len(all))
	for _, p := range all {
		fmt.Printf("  - %s in %s\n", p.Name, p.CurrentRoomID)
	}
	return nil
}
