// Package config holds the file locations the entrypoints share. The
// Go binaries read the same world JSON and schema the Python code does,
// so the defaults are relative to the project root -- run them from
// there (go/run.sh does).
package config

import "os"

// WorldDir is MUD_WORLD_DIR, default "src/world".
func WorldDir() string {
	return envOr("MUD_WORLD_DIR", "src/world")
}

// SchemaFile is MUD_SCHEMA_FILE, default "src/sql/schema.sql".
func SchemaFile() string {
	return envOr("MUD_SCHEMA_FILE", "src/sql/schema.sql")
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
