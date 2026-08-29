// Package endpoints registers where a customer wants webhooks delivered.
//
// This file is the data access. It holds no rule: it translates between the
// domain struct and the generated sqlc types, and it receives an executor
// instead of opening a transaction, because the operation boundary belongs to
// whoever orchestrates it.
package endpoints

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/victorzix/vhook/internal/store/sqlc"
)

// Endpoint is the domain struct. It is neither the sqlc row nor the generated
// OpenAPI type: reusing either would couple the rule to a wire format.
type Endpoint struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	URL           string
	Status        string
	Secret        string // only filled where the spec says it is returned
	CreatedAt     time.Time
}

func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }
func goUUID(id pgtype.UUID) uuid.UUID { return uuid.UUID(id.Bytes) }

// fromRow converts without the secret: decrypting is the service's job,
// because only it holds the cipher.
func fromRow(row sqlc.Endpoint) Endpoint {
	return Endpoint{
		ID:            goUUID(row.ID),
		ApplicationID: goUUID(row.ApplicationID),
		URL:           row.Url,
		Status:        row.Status,
		CreatedAt:     row.CreatedAt.Time,
	}
}

type repo struct{ q *sqlc.Queries }

func newRepo(db sqlc.DBTX) *repo { return &repo{q: sqlc.New(db)} }

func (r *repo) lockApplication(ctx context.Context, appID uuid.UUID) error {
	_, err := r.q.LockApplication(ctx, pgUUID(appID))
	return err
}

func (r *repo) count(ctx context.Context, appID uuid.UUID) (int64, error) {
	return r.q.CountEndpoints(ctx, pgUUID(appID))
}

func (r *repo) create(ctx context.Context, id, appID uuid.UUID, url string, blob []byte) (sqlc.Endpoint, error) {
	return r.q.CreateEndpoint(ctx, sqlc.CreateEndpointParams{
		ID: pgUUID(id), ApplicationID: pgUUID(appID), Url: url, SecretEncrypted: blob,
	})
}

func (r *repo) list(ctx context.Context, appID uuid.UUID) ([]sqlc.Endpoint, error) {
	return r.q.ListEndpoints(ctx, pgUUID(appID))
}

func (r *repo) get(ctx context.Context, appID, id uuid.UUID) (sqlc.Endpoint, error) {
	return r.q.GetEndpoint(ctx, sqlc.GetEndpointParams{ID: pgUUID(id), ApplicationID: pgUUID(appID)})
}

func (r *repo) updateURL(ctx context.Context, appID, id uuid.UUID, url string) (sqlc.Endpoint, error) {
	return r.q.UpdateEndpointURL(ctx, sqlc.UpdateEndpointURLParams{
		ID: pgUUID(id), ApplicationID: pgUUID(appID), Url: url,
	})
}
