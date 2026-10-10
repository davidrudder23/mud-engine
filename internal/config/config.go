// Package config holds the file locations the entrypoints share. The
// world JSON (world/) and schema (sql/) are read from disk at run time,
// so the defaults are relative to the project root -- run the binaries
// from there (run.sh does).
package config

import (
	"os"
	"strings"
)

// WorldDir is MUD_WORLD_DIR, default "world".
func WorldDir() string {
	return envOr("MUD_WORLD_DIR", "world")
}

// SchemaFile is MUD_SCHEMA_FILE, default "sql/schema.sql".
func SchemaFile() string {
	return envOr("MUD_SCHEMA_FILE", "sql/schema.sql")
}

// Wizzards is MUD_WIZZARDS: the comma-separated names of the players
// allowed to reload and unload scripts. Empty by default -- nobody is.
//
// There's no real auth (whatever name you type at login, you become
// that player), so this is only as strong as keeping the telnet port
// to people you trust.
func Wizzards() []string {
	var names []string
	for _, name := range strings.Split(os.Getenv("MUD_WIZZARDS"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// IsWizzard reports whether name is one of Wizzards, ignoring case.
func IsWizzard(name string) bool {
	for _, wizzard := range Wizzards() {
		if strings.EqualFold(wizzard, name) {
			return true
		}
	}
	return false
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
