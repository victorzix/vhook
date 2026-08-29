package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/victorzix/vhook/internal/apikey"
	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/store"
)

func migratedPostgres(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integração: precisa de Docker")
	}

	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
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
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return url
}

func setEnv(t *testing.T, dbURL string, master []byte) {
	t.Helper()
	t.Setenv("DATABASE_URL", dbURL)
	t.Setenv("VHOOK_MASTER_KEY", base64.StdEncoding.EncodeToString(master))
}

func countRows(t *testing.T, url, table string) int {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("contar %s: %v", table, err)
	}
	return n
}

func storedHash(t *testing.T, url string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var hash string
	if err := conn.QueryRow(ctx, "SELECT api_key_hash FROM applications").Scan(&hash); err != nil {
		t.Fatalf("ler hash: %v", err)
	}
	return hash
}

// printedKey pulls the api key out of the command output.
func printedKey(t *testing.T, out string) string {
	t.Helper()
	for _, field := range strings.Fields(out) {
		if strings.HasPrefix(field, apikey.Prefix) {
			return field
		}
	}
	t.Fatalf("nenhuma chave com prefixo %q na saída:\n%s", apikey.Prefix, out)
	return ""
}

var testMaster = []byte("0123456789abcdef0123456789abcdef")

func TestBootstrapCreatesOneOrganizationAndOneApplication(t *testing.T) {
	url := migratedPostgres(t)
	setEnv(t, url, testMaster)

	var out bytes.Buffer
	if err := bootstrap([]string{"--org", "Acme", "--app", "producao"}, &out); err != nil {
		t.Fatalf("bootstrap() error = %v", err)
	}

	if n := countRows(t, url, "organizations"); n != 1 {
		t.Errorf("organizations = %d, want 1", n)
	}
	if n := countRows(t, url, "applications"); n != 1 {
		t.Errorf("applications = %d, want 1", n)
	}
	if !strings.Contains(out.String(), "Acme") {
		t.Error("a saída não mostra o nome da organização")
	}
	if !strings.Contains(out.String(), "org_") || !strings.Contains(out.String(), "app_") {
		t.Error("a saída não mostra os ids na forma externa de §4.31")
	}
}

// O teste que prova que a chave impressa é utilizável. Sem ele o comando
// poderia imprimir uma chave e gravar o hash de outra, e só a spec de ingress
// descobriria — como um 401 em chave válida.
func TestStoredHashMatchesThePrintedKey(t *testing.T) {
	url := migratedPostgres(t)
	setEnv(t, url, testMaster)

	var out bytes.Buffer
	if err := bootstrap(nil, &out); err != nil {
		t.Fatalf("bootstrap() error = %v", err)
	}

	hasher, err := apikey.NewHasher(testMaster)
	if err != nil {
		t.Fatalf("NewHasher() error = %v", err)
	}
	if got, want := storedHash(t, url), hasher.Hash(printedKey(t, out.String())); got != want {
		t.Errorf("hash gravado = %s, queria %s", got, want)
	}
}

// Par de integração do teste de pepper: prova que a chave mestra atravessa o
// comando inteiro até a coluna, e não fica parada num parâmetro sem uso.
func TestADifferentMasterKeyProducesADifferentStoredHash(t *testing.T) {
	other := []byte("fedcba9876543210fedcba9876543210")

	first := migratedPostgres(t)
	setEnv(t, first, testMaster)
	var outA bytes.Buffer
	if err := bootstrap([]string{"--app", "igual"}, &outA); err != nil {
		t.Fatalf("primeiro bootstrap: %v", err)
	}

	second := migratedPostgres(t)
	setEnv(t, second, other)
	var outB bytes.Buffer
	if err := bootstrap([]string{"--app", "igual"}, &outB); err != nil {
		t.Fatalf("segundo bootstrap: %v", err)
	}

	if storedHash(t, first) == storedHash(t, second) {
		t.Error("chaves mestras diferentes produziram o mesmo hash gravado")
	}
}

func TestSecondRunRefusesAndChangesNothing(t *testing.T) {
	url := migratedPostgres(t)
	setEnv(t, url, testMaster)

	var first bytes.Buffer
	if err := bootstrap(nil, &first); err != nil {
		t.Fatalf("primeiro bootstrap: %v", err)
	}
	hashBefore := storedHash(t, url)

	var second bytes.Buffer
	err := bootstrap(nil, &second)
	if !errors.Is(err, errs.AlreadyBootstrapped) {
		t.Fatalf("error = %v, queria errs.AlreadyBootstrapped", err)
	}
	if n := countRows(t, url, "organizations"); n != 1 {
		t.Errorf("organizations = %d depois da recusa, want 1", n)
	}
	if storedHash(t, url) != hashBefore {
		t.Error("a recusa alterou o hash gravado")
	}
	if strings.Contains(second.String(), apikey.Prefix) {
		t.Error("a execução recusada imprimiu uma chave")
	}
}

func TestInvalidFlagsFailBeforeTouchingTheDatabase(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"locale fora dos quatro", []string{"--locale", "de"}},
		{"backoff profile inválido", []string{"--backoff-profile", "turbo"}},
		{"org vazia", []string{"--org", ""}},
		{"app vazia", []string{"--app", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := migratedPostgres(t)
			setEnv(t, url, testMaster)

			var out bytes.Buffer
			if err := bootstrap(tt.args, &out); !errors.Is(err, errs.InvalidArgument) {
				t.Fatalf("error = %v, queria errs.InvalidArgument", err)
			}
			if n := countRows(t, url, "organizations"); n != 0 {
				t.Errorf("organizations = %d, a validação devia vir antes do banco", n)
			}
		})
	}
}

// Prova a transação. Derrubar `applications` faz o primeiro insert passar e o
// segundo falhar — sem transação, sobraria uma organização órfã, e o comando
// recusaria corrigi-la na execução seguinte porque a organização já existiria.
func TestAFailureBetweenTheInsertsLeavesNothingBehind(t *testing.T) {
	url := migratedPostgres(t)
	setEnv(t, url, testMaster)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	if _, err := conn.Exec(ctx, "DROP TABLE applications CASCADE"); err != nil {
		t.Fatalf("derrubar applications: %v", err)
	}
	_ = conn.Close(ctx)

	var out bytes.Buffer
	if err := bootstrap(nil, &out); err == nil {
		t.Fatal("bootstrap devia ter falhado sem a tabela applications")
	}

	if n := countRows(t, url, "organizations"); n != 0 {
		t.Errorf("organizations = %d, a transação devia ter desfeito tudo", n)
	}
	if strings.Contains(out.String(), apikey.Prefix) {
		t.Error("imprimiu uma chave para uma transação que não fechou")
	}
}

// Duas execuções simultâneas em banco vazio: uma cria, a outra recusa ou falha
// na constraint. Nunca duas organizações.
func TestConcurrentBootstrapCreatesExactlyOneOrganization(t *testing.T) {
	url := migratedPostgres(t)
	setEnv(t, url, testMaster)

	const runs = 4
	errCh := make(chan error, runs)
	var wg sync.WaitGroup
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out bytes.Buffer
			errCh <- bootstrap(nil, &out)
		}()
	}
	wg.Wait()
	close(errCh)

	succeeded := 0
	for err := range errCh {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Errorf("%d execuções tiveram sucesso, queria exatamente 1", succeeded)
	}
	if n := countRows(t, url, "organizations"); n != 1 {
		t.Errorf("organizations = %d, want 1", n)
	}
}

func TestDefaultsMatchTheSpec(t *testing.T) {
	url := migratedPostgres(t)
	setEnv(t, url, testMaster)

	var out bytes.Buffer
	if err := bootstrap(nil, &out); err != nil {
		t.Fatalf("bootstrap() error = %v", err)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var name, plan, locale, backoff string
	err = conn.QueryRow(ctx,
		`SELECT name, plan, locale, backoff_profile FROM applications`).
		Scan(&name, &plan, &locale, &backoff)
	if err != nil {
		t.Fatalf("ler application: %v", err)
	}

	if name != "default" || plan != "free" || locale != "pt-BR" || backoff != "production" {
		t.Errorf("defaults = %q %q %q %q, queria default free pt-BR production",
			name, plan, locale, backoff)
	}
}
