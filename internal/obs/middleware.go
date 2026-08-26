package obs

import (
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/ids"
)

// A client-sent trace value is untrusted text that ends up in logs. Bounding
// length and alphabet keeps it from carrying newlines or control characters
// into a log line.
var clientCorrelationFormat = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Correlation puts a fresh trace id on every request and echoes it back on
// every response, success or failure. Without it there is no way to
// investigate a reported case, because error responses carry no message.
func Correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := ids.New()
		if err != nil {
			// Losing the trace id is not worth failing a request over.
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		rendered := ids.Render(id)

		w.Header().Set(HeaderCorrelationID, rendered)
		next.ServeHTTP(w, r.WithContext(WithCorrelationID(r.Context(), rendered)))
	})
}

// ValidClientCorrelationID reports whether the client's value is safe to log.
func ValidClientCorrelationID(v string) bool {
	return clientCorrelationFormat.MatchString(v)
}

// statusRecorder remembers the status code so the request log can report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// RequestLog emits one structured line per request. It is what makes the life
// of an event reconstructable from logs: the correlation id here is the same
// one the response carries and the same one an error body reports.
func RequestLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			clientID := r.Header.Get(HeaderClientCorrelationID)
			LogRequest(logger, clientID, ValidClientCorrelationID(clientID)).Info(
				"request",
				"correlation_id", CorrelationID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(started).Milliseconds(),
			)
		})
	}
}

// Recover turns a panic into the error envelope. The panic value and the
// stack go to the log and never to the response: in a system whose worker
// talks to the internal network, a leaked stack is leaked topology.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				clientID := r.Header.Get(HeaderClientCorrelationID)
				LogRequest(logger, clientID, ValidClientCorrelationID(clientID)).Error(
					"panic recovered",
					"code", errs.Internal.Code,
					"correlation_id", CorrelationID(r.Context()),
					"method", r.Method,
					"path", r.URL.Path,
					"panic", v,
					"stack", string(debug.Stack()),
				)
				WriteError(w, r, errs.Internal)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
