// Command demo-world-players shows room <-> player occupancy staying in
// sync: loads the world, creates two demo players in Postgres, places
// them into the (in-memory) world, moves one, and prints room occupant
// lists before/after. Run migrate once first to create the schema.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"

	"mudengine/internal/config"
	"mudengine/internal/db"
	"mudengine/internal/repository"
	"mudengine/internal/telnet"
	"mudengine/internal/world"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printOccupancy(w *world.World) {
	for _, room := range w.Rooms() {
		var names []string
		for _, p := range room.Players {
			names = append(names, p.Name)
		}
		if len(names) == 0 {
			fmt.Printf("  [%s] occupants: (empty)\n", room.ID)
		} else {
			fmt.Printf("  [%s] occupants: %v\n", room.ID, names)
		}
	}
}

func run() error {
	w, err := world.Load(config.WorldDir())
	if err != nil {
		return err
	}
	// The first two rooms connected by a doorway.
	start := w.Rooms()[0]
	if len(start.Doorways) == 0 {
		return fmt.Errorf("room %q has no doorways to demo a move through", start.ID)
	}
	next := start.Doorways[0].To

	ctx := context.Background()
	pool, err := db.Open(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo := repository.NewPostgresPlayerRepository(pool)

	suffix := uuid.NewString()[:8]
	alice, err := repo.Create(ctx, "alice-"+suffix, telnet.DefaultStartingRoomID(w))
	if err != nil {
		return err
	}
	bob, err := repo.Create(ctx, "bob-"+suffix, telnet.DefaultStartingRoomID(w))
	if err != nil {
		return err
	}

	// Bring persisted players into the running world.
	if err := w.PlacePlayer(alice); err != nil {
		return err
	}
	if err := w.PlacePlayer(bob); err != nil {
		return err
	}
	fmt.Printf("After both players join in %s:\n", start.ID)
	printOccupancy(w)

	if err := w.MovePlayer(bob, next); err != nil {
		return err
	}
	if err := repo.Save(ctx, bob); err != nil { // persist the move
		return err
	}
	fmt.Printf("\nAfter bob moves to %s:\n", next)
	printOccupancy(w)

	if bob.CurrentRoomID != next || alice.CurrentRoomID != start.ID {
		return fmt.Errorf("occupancy out of sync")
	}
	fmt.Println("\nOK: room occupant lists and CurrentRoomID agree.")
	return nil
}
