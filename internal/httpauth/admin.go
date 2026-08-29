// Package httpauth verifies the credentials of inbound requests.
package httpauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3filter"

	"github.com/victorzix/vhook/internal/errs"
)

// scheme is matched exactly, case included. RFC 7235 says the scheme token is
// case-insensitive, so this is stricter than the RFC on purpose: every client
// of this API is ours, they all send "Bearer", and a narrower accepted surface
// is one less shape to reason about.
const scheme = "Bearer "

// CheckAdminToken verifies the management credential.
//
// Both sides are hashed before comparing. subtle.ConstantTimeCompare is only
// constant-time for equal lengths — it returns early when they differ — so
// comparing digests keeps the timing flat whatever the attacker sends. A plain
// == would return at the first differing byte and leak the token prefix.
func CheckAdminToken(r *http.Request, expected string) error {
	// An unset expected token must never authorise anything: an API left open
	// by an empty environment variable is worse than one that refuses everyone.
	if expected == "" {
		return errors.Join(errs.InvalidCredentials,
			errors.New("httpauth: no admin token configured"))
	}

	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, scheme) {
		return errors.Join(errs.InvalidCredentials,
			errors.New("httpauth: missing or malformed authorization header"))
	}

	got := sha256.Sum256([]byte(strings.TrimPrefix(header, scheme)))
	want := sha256.Sum256([]byte(expected))
	if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
		// Same error as every other rejection, on purpose.
		return errors.Join(errs.InvalidCredentials, errors.New("httpauth: invalid admin token"))
	}
	return nil
}

// Authenticator dispatches on the security scheme declared in the contract.
// Adding a route with `security: [AdminToken]` protects it automatically —
// forgetting to protect a route stops being possible, because the contract
// says so and this function reads the contract.
func Authenticator(adminToken string) openapi3filter.AuthenticationFunc {
	return func(_ context.Context, in *openapi3filter.AuthenticationInput) error {
		switch in.SecuritySchemeName {
		case "AdminToken":
			return CheckAdminToken(in.RequestValidationInput.Request, adminToken)
		default:
			return errors.Join(errs.InvalidCredentials,
				fmt.Errorf("httpauth: unknown security scheme %q", in.SecuritySchemeName))
		}
	}
}
