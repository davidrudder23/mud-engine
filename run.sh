#!/bin/sh
# Build and run the telnet server. Runs from the project root, since
# the world JSON, schema, and llm.config.json paths are relative to it.
set -e
cd "$(dirname "$0")"
# Who may reload and unload Lua scripts (comma-separated player names).
# Set MUD_WIZZARDS yourself to override.
export MUD_WIZZARDS="${MUD_WIZZARDS:-drig}"
go build -o bin/ ./cmd/server
exec bin/server
