package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/victorzix/vhook/internal/dispatch"
	"github.com/victorzix/vhook/internal/endpoints"
	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/httpauth"
	"github.com/victorzix/vhook/internal/obs"
	"github.com/victorzix/vhook/internal/openapi"
	"github.com/victorzix/vhook/internal/secrets"
	"github.com/victorzix/vhook/internal/store/sqlc"
)

// apiServer satisfies the generated ServerInterface by promotion: obs.Health
// owns the three operational routes, endpoints.Handler the four management
// ones. Neither type knows about the other.
type apiServer struct {
	*obs.Health
	*endpoints.Handler
}

// buildRouter is the single wiring path of the api. main and the end-to-end
// test both call it, so a test can never pass against a router that production
// does not mount.
func buildRouter(logger *slog.Logger, health *obs.Health, pool *pgxpool.Pool, cfg config) (http.Handler, error) {
	cipher, err := secrets.NewCipher(cfg.masterKey)
	if err != nil {
		return nil, err
	}
	// The real resolver: net.DefaultResolver already has LookupNetIP with the
	// signature dispatch.Resolver declares.
	guard := dispatch.NewURLGuard(net.DefaultResolver, cfg.ssrfAllowlist)
	handler := endpoints.NewHandler(endpoints.NewService(pool, cipher, guard))

	return newRouter(logger, health, handler, cfg.adminToken)
}

// newRouter wires the middleware chain and the generated route table.
func newRouter(logger *slog.Logger, health *obs.Health, h *endpoints.Handler, adminToken string) (http.Handler, error) {
	// The spec is decoded once, at boot: the validation middleware panics if
	// the router cannot be built from it, and a panic belongs on the startup
	// path, not on a request.
	spec, err := openapi.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("api: load spec: %w", err)
	}

	r := chi.NewRouter()
	// A ordem importa: Correlation primeiro, para que o id já exista quando
	// RequestLog e Recover forem escrever.
	r.Use(obs.Correlation)
	r.Use(obs.RequestLog(logger))
	r.Use(obs.Recover(logger))

	// Validation and authentication both come from the contract. Adding a
	// route with `security: [AdminToken]` protects it automatically; there is
	// no hand-written list of protected paths to forget to update.
	r.Use(nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: httpauth.Authenticator(adminToken),
		},
		// The servers block is documentation; validating against it would
		// reject every request that does not carry the declared host.
		DoNotValidateServers: true,
		// The validator's own message never reaches the client: our error
		// envelope carries a code and a correlation id, never text.
		ErrorHandlerWithOpts: validationErrorHandler,
	}))

	// Without ErrorHandlerFunc the generated wrapper answers a bad path
	// parameter with http.Error and free text, outside the error envelope.
	return openapi.HandlerWithOptions(apiServer{Health: health, Handler: h}, openapi.ChiServerOptions{
		BaseRouter: r,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			obs.WriteError(w, r, errs.MalformedID)
		},
	}), nil
}

// validationErrorHandler turns what the contract validator rejected into the
// project's error envelope. The error text is deliberately dropped: it names
// schema internals, and the dashboard translates the code instead.
func validationErrorHandler(_ context.Context, _ error, w http.ResponseWriter, r *http.Request,
	opts nethttpmiddleware.ErrorHandlerOpts) {
	switch {
	case opts.StatusCode == http.StatusUnauthorized, opts.StatusCode == http.StatusForbidden:
		// Missing and wrong credentials are indistinguishable, on purpose.
		obs.WriteError(w, r, errs.InvalidCredentials)
	case opts.MatchedRoute == nil:
		// The path is not in the contract, so the generated router has no
		// route for it either. Answer what the router itself would, rather
		// than inventing an error code for a path that does not exist.
		http.NotFound(w, r)
	default:
		obs.WriteError(w, r, errs.MalformedID)
	}
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
