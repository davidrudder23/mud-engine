package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"mudengine/internal/logging"
	"mudengine/internal/world"
)

var logger = logging.For("mud.repository")

const selectColumns = "id, name, current_room_id, created_at, updated_at"

// uniqueViolation is Postgres' SQLSTATE for a unique constraint
// failure.
const uniqueViolation = "23505"

// PostgresPlayerRepository is the Postgres implementation of
// PlayerRepository.
type PostgresPlayerRepository struct {
	pool *pgxpool.Pool
}

var _ PlayerRepository = (*PostgresPlayerRepository)(nil)

func NewPostgresPlayerRepository(pool *pgxpool.Pool) *PostgresPlayerRepository {
	return &PostgresPlayerRepository{pool: pool}
}

func scanPlayer(row pgx.Row) (*world.Player, error) {
	var (
		id     pgtype.UUID
		player world.Player
	)
	if err := row.Scan(&id, &player.Name, &player.CurrentRoomID, &player.CreatedAt, &player.UpdatedAt); err != nil {
		return nil, err
	}
	player.ID = uuid.UUID(id.Bytes)
	return &player, nil
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// failed logs a DB error (other than "no rows", which is a normal
// outcome) and wraps it with op.
func failed(op string, err error) error {
	logger.Warn("DB operation failed", "op", op, "error", err)
	return fmt.Errorf("%s: %w", op, err)
}

func (r *PostgresPlayerRepository) Create(ctx context.Context, name, startingRoomID string) (*world.Player, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO players (name, current_room_id) VALUES ($1, $2) RETURNING `+selectColumns,
		name, startingRoomID)
	player, err := scanPlayer(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return nil, fmt.Errorf("%w: %q", ErrDuplicatePlayerName, name)
		}
		return nil, failed("create player", err)
	}
	return player, nil
}

func (r *PostgresPlayerRepository) getOne(ctx context.Context, op, where string, arg any) (*world.Player, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM players WHERE `+where, arg)
	player, err := scanPlayer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, failed(op, err)
	}
	return player, nil
}

func (r *PostgresPlayerRepository) GetByID(ctx context.Context, id uuid.UUID) (*world.Player, error) {
	return r.getOne(ctx, "get player by id", "id = $1", pgUUID(id))
}

func (r *PostgresPlayerRepository) GetByName(ctx context.Context, name string) (*world.Player, error) {
	return r.getOne(ctx, "get player by name", "name = $1", name)
}

func (r *PostgresPlayerRepository) Save(ctx context.Context, player *world.Player) error {
	if player.ID == uuid.Nil {
		return errors.New("cannot save a player that hasn't been created yet")
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE players SET current_room_id = $1, updated_at = now() WHERE id = $2`,
		player.CurrentRoomID, pgUUID(player.ID))
	if err != nil {
		return failed("save player", err)
	}
	return nil
}

func (r *PostgresPlayerRepository) ListAll(ctx context.Context) ([]*world.Player, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+selectColumns+` FROM players ORDER BY created_at`)
	if err != nil {
		return nil, failed("list players", err)
	}
	defer rows.Close()

	var players []*world.Player
	for rows.Next() {
		player, err := scanPlayer(rows)
		if err != nil {
			return nil, failed("list players", err)
		}
		players = append(players, player)
	}
	if err := rows.Err(); err != nil {
		return nil, failed("list players", err)
	}
	return players, nil
}
