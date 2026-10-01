// Package config holds the file locations the entrypoints share. The
// world JSON (world/) and schema (sql/) are read from disk at run time,
// so the defaults are relative to the project root -- run the binaries
// from there (run.sh does).
package config

import "os"

// WorldDir is MUD_WORLD_DIR, default "world".
func WorldDir() string {
	return envOr("MUD_WORLD_DIR", "world")
}

// SchemaFile is MUD_SCHEMA_FILE, default "sql/schema.sql".
func SchemaFile() string {
	return envOr("MUD_SCHEMA_FILE", "sql/schema.sql")
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
