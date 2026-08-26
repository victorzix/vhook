package obs_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/obs"
	"github.com/victorzix/vhook/internal/openapi"
)

func healthy(context.Context) error { return nil }
func broken(context.Context) error  { return errors.New("dial tcp: refused") }

func newTestHealth(t *testing.T, checks ...obs.Check) *obs.Health {
	t.Helper()
	return obs.NewHealth(obs.NewLogger(io.Discard, slog.LevelError), checks...)
}

func postgres(ping func(context.Context) error) obs.Check {
	return obs.Check{Name: "postgres", Err: errs.StorageUnavailable, Ping: ping}
}

func rabbitmq(ping func(context.Context) error) obs.Check {
	return obs.Check{Name: "rabbitmq", Err: errs.QueueUnavailable, Ping: ping}
}

func call(t *testing.T, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	obs.Correlation(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) openapi.Error {
	t.Helper()
	var body openapi.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo não é o envelope de erro: %v — %s", err, rec.Body.String())
	}
	return body
}

func TestHealthzNeverTouchesDependencies(t *testing.T) {
	called := false
	h := newTestHealth(t, postgres(func(context.Context) error {
		called = true
		return errors.New("não devia ter sido chamado")
	}))

	rec := call(t, h.GetHealth)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	// Liveness que consulta dependência faz o orquestrador matar um processo
	// saudável no primeiro blip do banco.
	if called {
		t.Error("/healthz consultou uma dependência")
	}
}

func TestReadyzReturnsOkWhenEverythingAnswers(t *testing.T) {
	h := newTestHealth(t, postgres(healthy), rabbitmq(healthy))

	rec := call(t, h.GetReadiness)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — corpo: %s", rec.Code, rec.Body.String())
	}
	var body openapi.Ready
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo inesperado: %v", err)
	}
	if string(body.Status) != "ready" {
		t.Errorf("status = %q, want ready", body.Status)
	}
	if string(body.Checks.Postgres) != "ok" || string(body.Checks.Rabbitmq) != "ok" {
		t.Errorf("checks = %+v", body.Checks)
	}
}

func TestReadyzReportsTheFailingDependency(t *testing.T) {
	tests := []struct {
		name     string
		checks   []obs.Check
		wantCode string
		wantLen  int
	}{
		{"só postgres fora", []obs.Check{postgres(broken), rabbitmq(healthy)}, "STO-DEP-001", 1},
		{"só rabbit fora", []obs.Check{postgres(healthy), rabbitmq(broken)}, "QUE-DEP-001", 1},
		// Ordem fixa de checagem: o code do topo é sempre o da primeira
		// falha na ordem, e details lista todas.
		{"ambos fora", []obs.Check{postgres(broken), rabbitmq(broken)}, "STO-DEP-001", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := call(t, newTestHealth(t, tt.checks...).GetReadiness)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", rec.Code)
			}
			body := decodeError(t, rec)
			if string(body.Error.Code) != tt.wantCode {
				t.Errorf("code = %q, want %q", body.Error.Code, tt.wantCode)
			}
			if body.Error.Details == nil || len(*body.Error.Details) != tt.wantLen {
				t.Fatalf("details = %v, queria %d entradas", body.Error.Details, tt.wantLen)
			}
			if body.Error.CorrelationId == "" {
				t.Error("correlation_id vazio")
			}
		})
	}
}

func TestReadyzNamesTheCheckInDetails(t *testing.T) {
	rec := call(t, newTestHealth(t, postgres(healthy), rabbitmq(broken)).GetReadiness)

	details := *decodeError(t, rec).Error.Details
	if details[0].Field != "rabbitmq" {
		t.Errorf("details[0].Field = %q, want rabbitmq", details[0].Field)
	}
	if string(details[0].Code) != "QUE-DEP-001" {
		t.Errorf("details[0].Code = %q, want QUE-DEP-001", details[0].Code)
	}
}

func TestReadyzTreatsASlowCheckAsAFailure(t *testing.T) {
	slow := func(ctx context.Context) error {
		<-ctx.Done() // o timeout de checagem é quem corta
		return ctx.Err()
	}
	h := obs.NewHealth(obs.NewLogger(io.Discard, slog.LevelError),
		obs.Check{Name: "postgres", Err: errs.StorageUnavailable, Ping: slow})
	h.SetCheckTimeout(50 * time.Millisecond)

	rec := call(t, h.GetReadiness)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if got := string(decodeError(t, rec).Error.Code); got != "STO-DEP-001" {
		t.Errorf("code = %q, want STO-DEP-001", got)
	}
}

func TestReadyzReportsDrainingBeforeCheckingAnything(t *testing.T) {
	called := false
	h := newTestHealth(t, postgres(func(context.Context) error {
		called = true
		return nil
	}))
	h.Drain()

	rec := call(t, h.GetReadiness)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := string(decodeError(t, rec).Error.Code); got != "SYS-DEP-001" {
		t.Errorf("code = %q, want SYS-DEP-001", got)
	}
	if called {
		t.Error("drenando, não faz sentido consultar dependência")
	}
}

func TestHealthzKeepsAnsweringWhileDraining(t *testing.T) {
	h := newTestHealth(t, postgres(healthy))
	h.Drain()

	// Liveness não muda no desligamento: o processo ainda está vivo, só
	// não quer requisição nova.
	if rec := call(t, h.GetHealth); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestMetricsExposesBuildInfoAndNoTenantLabel(t *testing.T) {
	obs.RegisterBuildInfo("v0.1.0", "abc1234")
	h := newTestHealth(t)

	rec := call(t, h.GetMetrics)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `vhook_build_info{`) {
		t.Error("vhook_build_info ausente")
	}
	if !strings.Contains(body, `version="v0.1.0"`) {
		t.Error("label version ausente")
	}
	if !strings.Contains(body, "go_goroutines") {
		t.Error("coletas padrão do runtime ausentes")
	}
	// Cardinalidade multiplicativa derruba o Prometheus antes do vhook.
	if strings.Contains(body, "application_id") {
		t.Error("métrica com label application_id")
	}
}

func TestHealthSatisfiesTheGeneratedInterface(t *testing.T) {
	var _ openapi.ServerInterface = newTestHealth(t)
}
