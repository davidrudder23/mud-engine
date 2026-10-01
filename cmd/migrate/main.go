// Command migrate applies the schema file (MUD_SCHEMA_FILE, default
// sql/schema.sql) to the configured database.
//
// A placeholder for real migrations -- fine while there's a single
// table, worth revisiting once the schema needs to evolve
// incrementally against data that already exists.
package main

import (
	"context"
	"fmt"
	"os"

	"mudengine/internal/config"
	"mudengine/internal/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	path := config.SchemaFile()
	sql, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := db.Open(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		return err
	}
	fmt.Println("Applied", path)
	return nil
}
