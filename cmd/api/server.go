package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/obs"
	"github.com/victorzix/vhook/internal/openapi"
	"github.com/victorzix/vhook/internal/store/sqlc"
)

// newRouter wires the middleware chain and the generated route table.
// Route groups with distinct authentication middleware arrive with the first
// authenticated surface; these three routes are public by nature.
func newRouter(logger *slog.Logger, health *obs.Health) http.Handler {
	r := chi.NewRouter()
	// A ordem importa: Correlation primeiro, para que o id já exista quando
	// RequestLog e Recover forem escrever.
	r.Use(obs.Correlation)
	r.Use(obs.RequestLog(logger))
	r.Use(obs.Recover(logger))
	return openapi.HandlerFromMux(health, r)
}

// postgresCheck runs a real query rather than pool.Ping: Ping only proves the
// connection is open, a query proves the database answers.
func postgresCheck(pool *pgxpool.Pool) obs.Check {
	return obs.Check{
		Name: "postgres",
		Err:  errs.StorageUnavailable,
		Ping: func(ctx context.Context) error {
			_, err := sqlc.New(pool).Ping(ctx)
			return err
		},
	}
}

// rabbitCheck dials a fresh connection each probe. This release publishes
// nothing, so there is no long-lived connection to keep alive; the persistent
// connection and its reconnect logic arrive with the queue spec.
func rabbitCheck(url string) obs.Check {
	return obs.Check{
		Name: "rabbitmq",
		Err:  errs.QueueUnavailable,
		Ping: func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			timeout := 2 * time.Second
			if ok {
				timeout = time.Until(deadline)
			}
			conn, err := amqp.DialConfig(url, amqp.Config{
				Dial: amqp.DefaultDial(timeout),
			})
			if err != nil {
				return err
			}
			return conn.Close()
		},
	}
}
