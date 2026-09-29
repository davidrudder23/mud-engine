#!/bin/sh
# Build and run the Go telnet server. Runs from the project root, since
# the world JSON, schema, and llm.config.json paths are relative to it.
set -e
cd "$(dirname "$0")/.."
go build -C go -o bin/ ./cmd/server
MUD_COMMAND_MATCHING=llm exec go/bin/server
