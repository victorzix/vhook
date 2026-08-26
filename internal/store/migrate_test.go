package store_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/victorzix/vhook/internal/store"
)

func tableNames(t *testing.T, ctx context.Context, url string) map[string]bool {
	t.Helper()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	rows, err := conn.Query(ctx, `
		SELECT tablename FROM pg_tables
		WHERE schemaname = 'public' AND tablename <> 'schema_migrations'`)
	if err != nil {
		t.Fatalf("listar tabelas: %v", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[name] = true
	}
	return out
}

func indexNames(t *testing.T, ctx context.Context, url string) map[string]bool {
	t.Helper()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	rows, err := conn.Query(ctx,
		`SELECT indexname FROM pg_indexes WHERE schemaname = 'public'`)
	if err != nil {
		t.Fatalf("listar índices: %v", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[name] = true
	}
	return out
}

func TestMigrateCreatesTheWholeSchema(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(t)

	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	want := []string{
		"organizations", "applications", "endpoints",
		"events", "deliveries", "delivery_attempts",
	}
	got := tableNames(t, ctx, url)
	for _, name := range want {
		if !got[name] {
			t.Errorf("falta a tabela %s", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("tabelas a mais: %v", got)
	}
}

func TestMigrateCreatesTheNamedIndexes(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(t)

	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Índices que a spec nomeia porque criá-los depois, sobre tabela em uso,
	// exigiria CREATE INDEX CONCURRENTLY.
	want := []string{
		"endpoints_application_id_idx",
		"deliveries_keyset_idx",
		"deliveries_reconciler_idx",
	}
	got := indexNames(t, ctx, url)
	for _, name := range want {
		if !got[name] {
			t.Errorf("falta o índice %s", name)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(t)

	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("primeira Migrate() error = %v", err)
	}
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("segunda Migrate() error = %v", err)
	}
}

func TestRollbackEmptiesTheSchema(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(t)

	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := store.Rollback(ctx, url); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	// Prova que os arquivos .down.sql não são ficção.
	if got := tableNames(t, ctx, url); len(got) != 0 {
		t.Errorf("sobraram tabelas depois do rollback: %v", got)
	}
}

func TestConcurrentMigrateSerializesUnderTheAdvisoryLock(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(t)

	// Duas instâncias da api subindo ao mesmo tempo. Uma aplica, a outra
	// espera o lock e segue; nenhuma pode errar.
	const instances = 4
	errCh := make(chan error, instances)
	var wg sync.WaitGroup
	for i := 0; i < instances; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- store.Migrate(ctx, url)
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("Migrate() concorrente error = %v", err)
		}
	}
}
