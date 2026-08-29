// Package errs is the error registry: code, level and HTTP status, and no
// text at all. Text lives in the i18n catalogue, indexed by code. Keeping the
// two apart is what stops them from drifting. See docs/ERRORS.md.
package errs

import "net/http"

// Level is the default logging level carried by an error. A call site may
// escalate it when the context justifies — an endpoint answering 503 is warn,
// the same endpoint tripping the circuit breaker is error.
type Level string

const (
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Type is a failure class. It supplies the default level and HTTP status so
// that neither is decided ad hoc at each call site.
type Type struct {
	Code   string
	Level  Level
	Status int
}

var (
	TypeVAL = Type{"VAL", LevelWarn, http.StatusUnprocessableEntity}
	TypeCRD = Type{"CRD", LevelWarn, http.StatusUnauthorized}
	TypePRM = Type{"PRM", LevelWarn, http.StatusForbidden}
	TypeNFD = Type{"NFD", LevelWarn, http.StatusNotFound}
	TypeCFL = Type{"CFL", LevelWarn, http.StatusConflict}
	TypeLMT = Type{"LMT", LevelWarn, http.StatusTooManyRequests}
	TypeDEP = Type{"DEP", LevelError, http.StatusBadGateway}
	TypeTMO = Type{"TMO", LevelError, http.StatusGatewayTimeout}
	TypeINT = Type{"INT", LevelError, http.StatusInternalServerError}
)

// Error is a registered failure. It is compared with errors.Is against the
// constant, never by matching message text.
type Error struct {
	Code       string
	Level      Level
	HTTPStatus int
}

func (e *Error) Error() string { return e.Code }

type option func(*Error)

// withStatus overrides the status the type would supply. Every use needs a
// reason recorded in the spec that introduced it.
func withStatus(s int) option { return func(e *Error) { e.HTTPStatus = s } }

// withLevel overrides the level the type would supply.
func withLevel(l Level) option { return func(e *Error) { e.Level = l } }

// noHTTPStatus marks an error that never becomes a response.
func noHTTPStatus() option { return func(e *Error) { e.HTTPStatus = 0 } }

var registry []*Error

func register(code string, t Type, opts ...option) *Error {
	e := &Error{Code: code, Level: t.Level, HTTPStatus: t.Status}
	for _, opt := range opts {
		opt(e)
	}
	registry = append(registry, e)
	return e
}

// All returns every registered error. The completeness test walks it.
func All() []*Error { return registry }

var (
	// StorageUnavailable overrides DEP's 502: 502 means "an upstream answered
	// badly"; this means "not ready to serve", which is 503 to orchestrators.
	StorageUnavailable = register("STO-DEP-001", TypeDEP, withStatus(http.StatusServiceUnavailable))

	// QueueUnavailable overrides DEP's 502 for the same reason.
	QueueUnavailable = register("QUE-DEP-001", TypeDEP, withStatus(http.StatusServiceUnavailable))

	// Draining overrides DEP's error level: shutting down is the normal path
	// of a deploy, and logging it as error trains operators to ignore error.
	Draining = register("SYS-DEP-001", TypeDEP,
		withStatus(http.StatusServiceUnavailable), withLevel(LevelWarn))

	Internal = register("SYS-INT-001", TypeINT)

	// MissingConfig never becomes a response: the process exits before the
	// port opens. It carries a level for the log, and that level overrides
	// VAL's warn — a boot the operator has to fix is not a client mistake.
	MissingConfig = register("CFG-VAL-001", TypeVAL,
		noHTTPStatus(), withLevel(LevelError))

	// AlreadyBootstrapped: the bootstrap command refuses to run twice. The
	// plaintext key exists only at the moment it is generated, so a second run
	// that silently recreated would orphan the previous application.
	AlreadyBootstrapped = register("APP-CFL-001", TypeCFL)

	// InvalidArgument: a CLI flag carries a value outside the allowed set.
	InvalidArgument = register("APP-VAL-001", TypeVAL)
)
