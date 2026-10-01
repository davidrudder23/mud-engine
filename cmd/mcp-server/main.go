// Command mcp-server runs the MUD as an MCP server (stdio transport) --
// one tool per command, one player for the whole process. See
// internal/mcpserver for how tools are built from commands.
//
// An MCP client spawns this process and talks JSON-RPC over its
// stdin/stdout, so nothing here may ever write to stdout directly --
// logging goes to stderr.
//
// Config via env vars:
//
//	MUD_MCP_PLAYER_NAME   default "adventurer" -- the single Player this
//	                      server plays as. Resolved the same way a telnet
//	                      login resolves a name.
//	MUD_WORLD_DIR         default world
//
// plus MUD_DB_*, MUD_LLM_*, and MUD_LOG_LEVEL.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mudengine/internal/config"
	"mudengine/internal/db"
	"mudengine/internal/logging"
	"mudengine/internal/mcpserver"
	"mudengine/internal/repository"
	"mudengine/internal/world"
)

var logger = logging.For("mud.mcp_server")

func main() {
	if err := run(); err != nil {
		logger.Error("MCP server failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logging.Configure()

	w, err := world.Load(config.WorldDir())
	if err != nil {
		return err
	}
	logger.Info("Loaded world", "structures", len(w.Structures), "rooms", w.Len(), "npcs", len(w.NPCs))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	playerName := os.Getenv("MUD_MCP_PLAYER_NAME")
	if playerName == "" {
		playerName = "adventurer"
	}

	server, err := mcpserver.BuildServer(ctx, w, repository.NewPostgresPlayerRepository(pool), playerName, mcpserver.Options{})
	if err != nil {
		return err
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}
