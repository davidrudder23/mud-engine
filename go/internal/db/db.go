// Package db handles database connections for player persistence.
//
// Config comes from environment variables, so the same code works
// across environments without changes:
//
//	MUD_DB_HOST      unset by default -> connect over the local Unix
//	                 socket using peer auth (matches your OS user)
//	MUD_DB_PORT      default 5432
//	MUD_DB_NAME      default "mud"
//	MUD_DB_USER      unset by default -> your OS user (peer auth)
//	MUD_DB_PASSWORD  unset by default
//
// A small connection pool is used rather than one connection per call:
// a multiplayer server needs many concurrent DB operations in flight.
package db

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"mudengine/internal/logging"
)

var logger = logging.For("mud.db")

const maxConns = 10

// Config builds the pool config from MUD_DB_* env vars. Only
// host/port/user/password that are set explicitly override pgx's own
// defaults, so the default stays a local Unix-socket connection using
// peer auth.
func Config() (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("db config: %w", err)
	}
	cfg.MaxConns = maxConns

	cfg.ConnConfig.Database = "mud"
	if name := os.Getenv("MUD_DB_NAME"); name != "" {
		cfg.ConnConfig.Database = name
	}
	if host := os.Getenv("MUD_DB_HOST"); host != "" {
		cfg.ConnConfig.Host = host
		cfg.ConnConfig.Fallbacks = nil
	}
	if port := os.Getenv("MUD_DB_PORT"); port != "" {
		p, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("db config: invalid MUD_DB_PORT %q: %w", port, err)
		}
		cfg.ConnConfig.Port = uint16(p)
		for _, fallback := range cfg.ConnConfig.Fallbacks {
			fallback.Port = uint16(p)
		}
	}
	if user := os.Getenv("MUD_DB_USER"); user != "" {
		cfg.ConnConfig.User = user
	}
	if password := os.Getenv("MUD_DB_PASSWORD"); password != "" {
		cfg.ConnConfig.Password = password
	}
	return cfg, nil
}

// Open creates the connection pool. Connections are made lazily, on
// first use.
func Open(ctx context.Context) (*pgxpool.Pool, error) {
	cfg, err := Config()
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db pool: %w", err)
	}
	// host/port/dbname only -- never log user/password.
	logger.Info("Created DB connection pool",
		"dbname", cfg.ConnConfig.Database, "host", cfg.ConnConfig.Host, "port", cfg.ConnConfig.Port)
	return pool, nil
}
