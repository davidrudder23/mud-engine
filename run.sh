#!/bin/sh
# Build and run the telnet server. Runs from the project root, since
# the world JSON, schema, and llm.config.json paths are relative to it.
set -e
cd "$(dirname "$0")"
go build -o bin/ ./cmd/server
exec bin/server
