package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/victorzix/vhook/internal/store"
)

type idVector struct {
	Name   string `json:"name"`
	UUID   string `json:"uuid"`
	Base32 string `json:"base32"`
}

// Os vetores são os mesmos de internal/ids. Duas implementações do mesmo
// encoding só são seguras se forem provadas contra a mesma fonte.
func loadSharedVectors(t *testing.T) []idVector {
	t.Helper()
	raw, err := os.ReadFile("../ids/testdata/vectors.json")
	if err != nil {
		t.Fatalf("read shared vectors: %v", err)
	}
	var vs []idVector
	if err := json.Unmarshal(raw, &vs); err != nil {
		t.Fatalf("parse shared vectors: %v", err)
	}
	return vs
}

func migratedConn(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()
	url := startPostgres(t)
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func TestVhookIDMatchesTheSharedVectors(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t, ctx)

	for _, v := range loadSharedVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			var got uuid.UUID
			err := conn.QueryRow(ctx, `SELECT vhook_id($1)`, "evt_"+v.Base32).Scan(&got)
			if err != nil {
				t.Fatalf("vhook_id() error = %v", err)
			}
			if want := uuid.MustParse(v.UUID); got != want {
				t.Errorf("vhook_id() = %v, want %v", got, want)
			}
		})
	}
}

func TestVhookIDAcceptsInputVariations(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t, ctx)

	want := uuid.MustParse("018f4c2a-7b31-7c4e-9a2b-1f5c8d3e6b04")
	inputs := map[string]string{
		"com prefixo":     "evt_01HX62MYSHFH79MARZBJ6KWTR4",
		"sem prefixo":     "01HX62MYSHFH79MARZBJ6KWTR4",
		"outro prefixo":   "dlv_01HX62MYSHFH79MARZBJ6KWTR4",
		"minúsculas":      "evt_01hx62myshfh79marzbj6kwtr4",
		"crockford I L O": "evt_OIHX62MYSHFH79MARZBJ6KWTR4",
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			var got uuid.UUID
			if err := conn.QueryRow(ctx, `SELECT vhook_id($1)`, input).Scan(&got); err != nil {
				t.Fatalf("vhook_id(%q) error = %v", input, err)
			}
			if got != want {
				t.Errorf("vhook_id(%q) = %v, want %v", input, got, want)
			}
		})
	}
}

func TestVhookIDRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t, ctx)

	inputs := map[string]string{
		"curto demais": "evt_01HX62MYSHFH79MARZBJ6KWTR",
		"longo demais": "evt_01HX62MYSHFH79MARZBJ6KWTR44",
		"letra U":      "evt_01HX62MYSHFH79MARZBJ6KWTU4",
		"estouro":      "evt_8ZZZZZZZZZZZZZZZZZZZZZZZZZ",
		"lixo":         "não é um id",
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			var got uuid.UUID
			err := conn.QueryRow(ctx, `SELECT vhook_id($1)`, input).Scan(&got)
			if err == nil {
				t.Fatalf("vhook_id(%q) devia falhar, devolveu %v", input, got)
			}
			// 22P02 é o mesmo SQLSTATE de 'x'::uuid: um typo aparece em vez
			// de virar NULL silencioso numa cláusula WHERE.
			if !isSQLState(err, "22P02") {
				t.Errorf("vhook_id(%q) error = %v, queria SQLSTATE 22P02", input, err)
			}
		})
	}
}

func isSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
