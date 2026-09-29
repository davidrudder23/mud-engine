// Command server runs the MUD telnet server.
//
// Connect with: telnet localhost 8023
//
// Config via env vars:
//
//	MUD_TELNET_HOST   default 127.0.0.1 (loopback only; set to 0.0.0.0
//	                  to accept connections from other machines)
//	MUD_TELNET_PORT   default 8023
//	MUD_WORLD_DIR     default src/world
//
// plus MUD_DB_* (internal/db), MUD_LLM_* (internal/llm),
// MUD_COMMAND_MATCHING (internal/telnet), and MUD_LOG_LEVEL.
package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"mudengine/internal/config"
	"mudengine/internal/db"
	"mudengine/internal/logging"
	"mudengine/internal/repository"
	"mudengine/internal/telnet"
	"mudengine/internal/world"
)

var logger = logging.For("mud.server")

func main() {
	if err := run(); err != nil {
		logger.Error("Server failed", "error", err)
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

	host := os.Getenv("MUD_TELNET_HOST")
	if host == "" {
		host = telnet.DefaultHost
	}
	port := telnet.DefaultPort
	if value := os.Getenv("MUD_TELNET_PORT"); value != "" {
		if port, err = strconv.Atoi(value); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	err = telnet.Run(ctx, w, repository.NewPostgresPlayerRepository(pool), host, port)
	if ctx.Err() != nil {
		logger.Info("Shutting down")
	}
	return err
}
