package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/victorzix/vhook/internal/apikey"
	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/ids"
	"github.com/victorzix/vhook/internal/store"
	"github.com/victorzix/vhook/internal/store/sqlc"
)

var (
	validLocales  = []string{"pt-BR", "en", "es", "fr"}
	validProfiles = []string{"production", "demo"}
)

const maxNameLength = 200

// bootstrapLockID is an arbitrary but fixed advisory lock key — "vhkboot" in
// ASCII. Every bootstrap run has to pick the same number, or they do not
// serialise against each other.
const bootstrapLockID int64 = 0x7668_6B62_6F6F74

type bootstrapFlags struct {
	org            string
	app            string
	locale         string
	backoffProfile string
}

func parseBootstrapFlags(args []string) (bootstrapFlags, error) {
	var f bootstrapFlags
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.StringVar(&f.org, "org", "vhook", "organization name")
	fs.StringVar(&f.app, "app", "default", "application name")
	fs.StringVar(&f.locale, "locale", "pt-BR", "one of pt-BR, en, es, fr")
	fs.StringVar(&f.backoffProfile, "backoff-profile", "production", "production or demo")
	if err := fs.Parse(args); err != nil {
		return bootstrapFlags{}, errors.Join(errs.InvalidArgument, err)
	}

	switch {
	case f.org == "" || len(f.org) > maxNameLength:
		return bootstrapFlags{}, errors.Join(errs.InvalidArgument,
			fmt.Errorf("bootstrap: --org must be 1..%d characters", maxNameLength))
	case f.app == "" || len(f.app) > maxNameLength:
		return bootstrapFlags{}, errors.Join(errs.InvalidArgument,
			fmt.Errorf("bootstrap: --app must be 1..%d characters", maxNameLength))
	case !slices.Contains(validLocales, f.locale):
		return bootstrapFlags{}, errors.Join(errs.InvalidArgument,
			fmt.Errorf("bootstrap: --locale must be one of %v", validLocales))
	case !slices.Contains(validProfiles, f.backoffProfile):
		return bootstrapFlags{}, errors.Join(errs.InvalidArgument,
			fmt.Errorf("bootstrap: --backoff-profile must be one of %v", validProfiles))
	}
	return f, nil
}

// pgUUID adapts the uuid.UUID that ids.New produces to the pgtype.UUID that
// sqlc generated for the uuid columns.
func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// bootstrap creates the first organization and application. It refuses to run
// twice: the plaintext key exists only at the moment it is generated, so a
// second run that recreated silently would orphan the previous application and
// leave it unreachable with nobody noticing.
func bootstrap(args []string, out io.Writer) error {
	// Flags first, so a typo never reaches the database.
	f, err := parseBootstrapFlags(args)
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	hasher, err := apikey.NewHasher(cfg.masterKey)
	if err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := store.NewPool(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.Join(errs.StorageUnavailable, fmt.Errorf("bootstrap: begin: %w", err))
	}
	// Rollback after a successful commit is a no-op, so this is safe as the
	// single cleanup path.
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)

	// Taken before the count, and released only when the transaction ends: two
	// runs against an empty database would otherwise both read zero and both
	// insert, since each mints a distinct api_key_hash and the UNIQUE on that
	// column never collides. The second run blocks here, then sees the
	// organization the first one committed and refuses.
	if err := q.LockBootstrap(ctx, bootstrapLockID); err != nil {
		return errors.Join(errs.StorageUnavailable, fmt.Errorf("bootstrap: lock: %w", err))
	}

	existing, err := q.CountOrganizations(ctx)
	if err != nil {
		return errors.Join(errs.StorageUnavailable, fmt.Errorf("bootstrap: count: %w", err))
	}
	if existing > 0 {
		return errors.Join(errs.AlreadyBootstrapped,
			errors.New("bootstrap: an organization already exists"))
	}

	orgID, err := ids.New()
	if err != nil {
		return fmt.Errorf("bootstrap: new organization id: %w", err)
	}
	appID, err := ids.New()
	if err != nil {
		return fmt.Errorf("bootstrap: new application id: %w", err)
	}

	plain, hash, err := hasher.Generate()
	if err != nil {
		return err
	}

	if _, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		ID:   pgUUID(orgID),
		Name: f.org,
	}); err != nil {
		return errors.Join(errs.StorageUnavailable,
			fmt.Errorf("bootstrap: create organization: %w", err))
	}

	if _, err := q.CreateApplication(ctx, sqlc.CreateApplicationParams{
		ID:             pgUUID(appID),
		OrganizationID: pgUUID(orgID),
		Name:           f.app,
		ApiKeyHash:     hash,
		Locale:         f.locale,
		BackoffProfile: f.backoffProfile,
	}); err != nil {
		return errors.Join(errs.StorageUnavailable,
			fmt.Errorf("bootstrap: create application: %w", err))
	}

	if err := tx.Commit(ctx); err != nil {
		return errors.Join(errs.StorageUnavailable, fmt.Errorf("bootstrap: commit: %w", err))
	}

	// Printed only after the commit: a key shown for a transaction that then
	// failed would send someone chasing a credential that does not exist.
	//
	// The writes discard their error on purpose. errcheck excludes os.Stderr
	// but not a generic io.Writer, and a failed write to stdout is not
	// something this command can act on — the transaction already committed.
	_, _ = fmt.Fprintf(out, "organization  %s  %s\n",
		ids.Encode(ids.Organization, orgID), f.org)
	_, _ = fmt.Fprintf(out, "application   %s  %s\n",
		ids.Encode(ids.Application, appID), f.app)
	_, _ = fmt.Fprintf(out, "              plan=free  locale=%s  backoff_profile=%s\n",
		f.locale, f.backoffProfile)
	_, _ = fmt.Fprintf(out, "api key       %s\n", plain)
	_, _ = fmt.Fprint(out, "              ^ shown once. It cannot be recovered.\n")
	return nil
}
