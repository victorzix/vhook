package endpoints

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/victorzix/vhook/internal/dispatch"
	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/ids"
	"github.com/victorzix/vhook/internal/secrets"
	"github.com/victorzix/vhook/internal/tokens"
)

const (
	// secretPrefix makes the secret recognisable in the customer's .env.
	secretPrefix = "whsec_"

	// secretLength is 43 because 43 × log2(62) = 256.0 bits.
	secretLength = 43

	// freePlanEndpoints is §4.28. It lives in code and not in the environment:
	// per-tenant behaviour belongs in the database, and this is the plan's
	// definition, not a deployment knob.
	freePlanEndpoints = 2
)

// Service owns the transaction boundary. The repo receives an executor and
// never opens one of its own.
type Service struct {
	pool   *pgxpool.Pool
	cipher *secrets.Cipher
	guard  *dispatch.URLGuard
}

func NewService(pool *pgxpool.Pool, cipher *secrets.Cipher, guard *dispatch.URLGuard) *Service {
	return &Service{pool: pool, cipher: cipher, guard: guard}
}

// Create registers an endpoint and returns its secret, which is the only time
// it comes back alongside creation.
func (s *Service) Create(ctx context.Context, appID uuid.UUID, rawURL string) (Endpoint, error) {
	// Validate before touching the database: a typo must never reach a
	// transaction, and the tests assert no row is written.
	if err := s.guard.Validate(ctx, rawURL); err != nil {
		return Endpoint{}, err
	}

	id, err := ids.New()
	if err != nil {
		return Endpoint{}, fmt.Errorf("endpoints: new id: %w", err)
	}
	secret, err := tokens.Random(secretPrefix, secretLength)
	if err != nil {
		return Endpoint{}, err
	}
	// The AAD is the endpoint's external id: a blob moved to another row
	// fails to open instead of opening fine.
	blob, err := s.cipher.Seal([]byte(secret), []byte(ids.Encode(ids.Endpoint, id)))
	if err != nil {
		return Endpoint{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()

	r := newRepo(tx)

	// The lock comes BEFORE the count. Without it two concurrent creates read
	// the same total and both insert.
	if err := r.lockApplication(ctx, appID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Endpoint{}, errors.Join(errs.ApplicationNotFound,
				fmt.Errorf("endpoints: application %s", ids.Encode(ids.Application, appID)))
		}
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: lock: %w", err))
	}

	n, err := r.count(ctx, appID)
	if err != nil {
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: count: %w", err))
	}
	if n >= freePlanEndpoints {
		return Endpoint{}, errors.Join(errs.EndpointLimit,
			fmt.Errorf("endpoints: plan allows %d", freePlanEndpoints))
	}

	row, err := r.create(ctx, id, appID, rawURL, blob)
	if err != nil {
		if isUniqueViolation(err) {
			return Endpoint{}, errors.Join(errs.DuplicateEndpoint,
				errors.New("endpoints: url already registered in this application"))
		}
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: create: %w", err))
	}

	if err := tx.Commit(ctx); err != nil {
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: commit: %w", err))
	}

	out := fromRow(row)
	out.Secret = secret
	return out, nil
}

func (s *Service) List(ctx context.Context, appID uuid.UUID) ([]Endpoint, error) {
	rows, err := newRepo(s.pool).list(ctx, appID)
	if err != nil {
		return nil, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: list: %w", err))
	}
	out := make([]Endpoint, 0, len(rows))
	for _, row := range rows {
		// No secret here: the list is the response that shows up in the most
		// places, and revealing belongs to the detail route.
		out = append(out, fromRow(row))
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, appID, id uuid.UUID) (Endpoint, error) {
	row, err := newRepo(s.pool).get(ctx, appID, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Also the answer for "exists, but belongs to another tenant".
			return Endpoint{}, errors.Join(errs.EndpointNotFound,
				fmt.Errorf("endpoints: %s", ids.Encode(ids.Endpoint, id)))
		}
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: get: %w", err))
	}

	secret, err := s.cipher.Open(row.SecretEncrypted, []byte(ids.Encode(ids.Endpoint, goUUID(row.ID))))
	if err != nil {
		// Wrong master key, or a blob moved between rows. Never return junk.
		return Endpoint{}, errors.Join(errs.Internal, fmt.Errorf("endpoints: open secret: %w", err))
	}

	out := fromRow(row)
	out.Secret = string(secret)
	return out, nil
}

func (s *Service) UpdateURL(ctx context.Context, appID, id uuid.UUID, rawURL string) (Endpoint, error) {
	if err := s.guard.Validate(ctx, rawURL); err != nil {
		return Endpoint{}, err
	}
	row, err := newRepo(s.pool).updateURL(ctx, appID, id, rawURL)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return Endpoint{}, errors.Join(errs.EndpointNotFound,
				fmt.Errorf("endpoints: %s", ids.Encode(ids.Endpoint, id)))
		case isUniqueViolation(err):
			return Endpoint{}, errors.Join(errs.DuplicateEndpoint,
				errors.New("endpoints: url already registered in this application"))
		default:
			return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: update: %w", err))
		}
	}
	return fromRow(row), nil
}

// isUniqueViolation recognises SQLSTATE 23505. Comparing the code and not the
// message keeps this working when Postgres rewords the text.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
