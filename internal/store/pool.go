package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/victorzix/vhook/internal/errs"
)

// NewPool opens the connection pool used by everything except the migration
// runner, which needs database/sql.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: parse DATABASE_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.Join(errs.StorageUnavailable, fmt.Errorf("store: pool: %w", err))
	}
	return pool, nil
}
