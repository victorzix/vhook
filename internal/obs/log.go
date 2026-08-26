// Package obs holds the cross-cutting observability surface: structured
// logging, the correlation id that follows a request across processes, the
// Prometheus endpoint and the health handlers.
package obs

import (
	"context"
	"io"
	"log/slog"
)

// Header names. The first is ours and always present; the second is what a
// producer may send so its own logs can be joined to ours.
const (
	HeaderCorrelationID       = "X-Vhook-Correlation-Id"
	HeaderClientCorrelationID = "X-Correlation-Id"
)

type ctxKey struct{}

// NewLogger returns the structured logger used by every process.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}

// WithCorrelationID puts the trace id on the context so services and repos
// can log it without taking an http.Request.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// CorrelationID returns the trace id, or "" outside a request.
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// LogRequest decorates a logger with what the client sent. A valid client
// value is recorded in its own field and never reused as our trace id; an
// invalid one is recorded only as having been dropped, because it is
// attacker-controlled text and does not belong in a searchable field.
func LogRequest(logger *slog.Logger, clientID string, valid bool) *slog.Logger {
	switch {
	case clientID == "":
		return logger
	case valid:
		return logger.With("client_correlation_id", clientID)
	default:
		return logger.With("client_correlation_id_dropped", true)
	}
}
