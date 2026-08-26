package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcrabbit "github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/victorzix/vhook/internal/obs"
	"github.com/victorzix/vhook/internal/store"
)

func TestServerAnswersTheThreeOperationalRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("integração: precisa de Docker")
	}
	ctx := context.Background()

	pg, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("vhook"),
		tcpostgres.WithUsername("vhook"),
		tcpostgres.WithPassword("vhook"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("subir postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })

	rabbit, err := tcrabbit.Run(ctx, "rabbitmq:4-management-alpine")
	if err != nil {
		t.Fatalf("subir rabbitmq: %v", err)
	}
	t.Cleanup(func() { _ = rabbit.Terminate(context.Background()) })

	dbURL, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	amqpURL, err := rabbit.AmqpURL(ctx)
	if err != nil {
		t.Fatalf("amqp url: %v", err)
	}

	if err := store.Migrate(ctx, dbURL); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	pool, err := store.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(pool.Close)

	logger := obs.NewLogger(io.Discard, slog.LevelError)
	obs.RegisterBuildInfo("v0.0.0-test", "test")
	health := obs.NewHealth(logger, postgresCheck(pool), rabbitCheck(amqpURL))
	router := newRouter(logger, health)

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	t.Run("healthz", func(t *testing.T) {
		res := get(t, server.URL+"/healthz")
		if res.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", res.StatusCode)
		}
		if res.Header.Get(obs.HeaderCorrelationID) == "" {
			t.Error("resposta sem X-Vhook-Correlation-Id")
		}
	})

	t.Run("readyz com tudo de pé", func(t *testing.T) {
		res := get(t, server.URL+"/readyz")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 — %s", res.StatusCode, readBody(t, res))
		}
	})

	t.Run("metrics", func(t *testing.T) {
		res := get(t, server.URL+"/metrics")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		if !strings.Contains(readBody(t, res), "vhook_build_info") {
			t.Error("vhook_build_info ausente")
		}
	})

	// Os dois subtestes abaixo matam containers e não os trazem de volta.
	// A ordem é deliberada: o Rabbit primeiro, porque derrubar o Postgres
	// faria o code do topo virar STO-DEP-001 e esconder o do Rabbit.
	t.Run("rabbitmq cai depois do boot", func(t *testing.T) {
		if err := rabbit.Stop(ctx, nil); err != nil {
			t.Fatalf("parar rabbitmq: %v", err)
		}

		if res := get(t, server.URL+"/healthz"); res.StatusCode != http.StatusOK {
			t.Errorf("/healthz = %d, want 200", res.StatusCode)
		}

		res := get(t, server.URL+"/readyz")
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("/readyz = %d, want 503", res.StatusCode)
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(readBody(t, res)), &body); err != nil {
			t.Fatalf("corpo inesperado: %v", err)
		}
		if body.Error.Code != "QUE-DEP-001" {
			t.Errorf("code = %q, want QUE-DEP-001", body.Error.Code)
		}
	})

	t.Run("postgres cai depois do boot", func(t *testing.T) {
		if err := pg.Stop(ctx, nil); err != nil {
			t.Fatalf("parar postgres: %v", err)
		}

		if res := get(t, server.URL+"/healthz"); res.StatusCode != http.StatusOK {
			t.Errorf("/healthz = %d, want 200 — liveness não olha dependência",
				res.StatusCode)
		}

		res := get(t, server.URL+"/readyz")
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("/readyz = %d, want 503", res.StatusCode)
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(readBody(t, res)), &body); err != nil {
			t.Fatalf("corpo inesperado: %v", err)
		}
		if body.Error.Code != "STO-DEP-001" {
			t.Errorf("code = %q, want STO-DEP-001", body.Error.Code)
		}
	})
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	res, err := http.Get(url) //nolint:noctx // teste local
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ler corpo: %v", err)
	}
	return string(raw)
}
