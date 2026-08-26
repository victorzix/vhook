package obs

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/openapi"
)

// defaultCheckTimeout bounds each readiness probe. A dependency that is slow
// past this is a dependency that is down as far as a load balancer cares.
const defaultCheckTimeout = 2 * time.Second

// Check is one readiness probe. Err is the registered error reported when the
// probe fails, which is how the code in the response stays actionable.
type Check struct {
	Name string
	Err  *errs.Error
	Ping func(ctx context.Context) error
}

// Health serves /healthz, /readyz and /metrics. It satisfies the generated
// openapi.ServerInterface.
type Health struct {
	logger   *slog.Logger
	checks   []Check
	draining atomic.Bool

	mu      sync.RWMutex
	timeout time.Duration
}

// NewHealth builds the handler. Checks are probed in the order given, and
// that order decides which code leads the error response.
func NewHealth(logger *slog.Logger, checks ...Check) *Health {
	return &Health{logger: logger, checks: checks, timeout: defaultCheckTimeout}
}

// SetCheckTimeout overrides the per-probe deadline. Tests use it; production
// keeps the default, because a probe budget is behaviour and behaviour lives
// in code, not in the environment.
func (h *Health) SetCheckTimeout(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.timeout = d
}

func (h *Health) checkTimeout() time.Duration {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.timeout
}

// Drain flips readiness to 503 while liveness keeps answering. Called on
// SIGTERM so a load balancer stops sending new requests before the server
// stops accepting connections.
func (h *Health) Drain() { h.draining.Store(true) }

// GetHealth is liveness. It never touches a dependency: a blip in Postgres
// must not make an orchestrator kill a healthy process.
func (h *Health) GetHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, openapi.Health{Status: openapi.HealthStatusOk})
}

// GetReadiness probes every dependency in a fixed order.
func (h *Health) GetReadiness(w http.ResponseWriter, r *http.Request) {
	if h.draining.Load() {
		WriteError(w, r, errs.Draining)
		return
	}

	var (
		first   *errs.Error
		details []openapi.ErrorDetail
	)
	for _, c := range h.checks {
		ctx, cancel := context.WithTimeout(r.Context(), h.checkTimeout())
		err := c.Ping(ctx)
		cancel()
		if err == nil {
			continue
		}
		if first == nil {
			first = c.Err
		}
		details = append(details, openapi.ErrorDetail{
			Field: c.Name,
			Code:  openapi.ErrorCode(c.Err.Code),
		})
		h.logger.Log(r.Context(), slog.LevelError, "readiness check failed",
			"code", c.Err.Code,
			"correlation_id", CorrelationID(r.Context()),
			"check", c.Name,
			"error", err.Error(),
		)
	}

	if first != nil {
		WriteError(w, r, first, details...)
		return
	}

	writeJSON(w, http.StatusOK, openapi.Ready{
		Status: openapi.ReadyStatusReady,
		Checks: openapi.ReadyChecks{
			Postgres: openapi.ReadyChecksPostgresOk,
			Rabbitmq: openapi.ReadyChecksRabbitmqOk,
		},
	})
}

// GetMetrics serves the Prometheus exposition format.
func (h *Health) GetMetrics(w http.ResponseWriter, r *http.Request) {
	promhttp.Handler().ServeHTTP(w, r)
}

var buildInfoOnce sync.Once

// RegisterBuildInfo publishes version and commit as the only vhook-owned
// metric of this release. No metric ever carries application_id: cardinality
// in Prometheus is multiplicative and takes the server down before vhook.
func RegisterBuildInfo(version, commit string) {
	buildInfoOnce.Do(func() {
		promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "vhook_build_info",
			Help: "Build metadata of the running binary.",
		}, []string{"version", "commit"}).WithLabelValues(version, commit).Set(1)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
