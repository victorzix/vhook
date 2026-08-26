// Package store owns everything that touches Postgres: the pool, the migration
// runner and the sqlc-generated code.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver named "pgx"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/migrations"
)

// Migrate applies every pending migration. golang-migrate takes a Postgres
// advisory lock for the duration, so two api instances booting at once
// serialise instead of racing: one applies, the other waits and moves on.
func Migrate(ctx context.Context, databaseURL string) error {
	return run(ctx, databaseURL, func(m *migrate.Migrate) error { return m.Up() })
}

// Rollback undoes every applied migration. Development and tests only.
func Rollback(ctx context.Context, databaseURL string) error {
	return run(ctx, databaseURL, func(m *migrate.Migrate) error { return m.Down() })
}

func run(ctx context.Context, databaseURL string, direction func(*migrate.Migrate) error) error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("store: read migrations: %w", err)
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("store: open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return errors.Join(errs.StorageUnavailable, fmt.Errorf("store: ping: %w", err))
	}

	driver, err := migratepg.WithInstance(db, &migratepg.Config{})
	if err != nil {
		return fmt.Errorf("store: migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "pgx", driver)
	if err != nil {
		return fmt.Errorf("store: migration runner: %w", err)
	}

	if err := direction(m); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}
