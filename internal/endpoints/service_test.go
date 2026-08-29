package endpoints_test

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/victorzix/vhook/internal/apikey"
	"github.com/victorzix/vhook/internal/dispatch"
	"github.com/victorzix/vhook/internal/endpoints"
	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/ids"
	"github.com/victorzix/vhook/internal/secrets"
	"github.com/victorzix/vhook/internal/store"
	"github.com/victorzix/vhook/internal/store/sqlc"
)

var master = []byte("0123456789abcdef0123456789abcdef")

type fakeResolver struct{}

func (fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	switch host {
	case "interno.exemplo.com":
		return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
	case "naoexiste.exemplo.com":
		return nil, errors.New("no such host")
	default:
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}
}

// newFixture sobe Postgres, migra, cria uma application e devolve o service.
func newFixture(t *testing.T) (*endpoints.Service, uuid.UUID, *pgxpool.Pool) {
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

	dbURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	if err := store.Migrate(ctx, dbURL); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	pool, err := store.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(pool.Close)

	appID := seedApplication(t, ctx, pool)

	cipher, err := secrets.NewCipher(master)
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	guard := dispatch.NewURLGuard(fakeResolver{}, nil)

	return endpoints.NewService(pool, cipher, guard), appID, pool
}

// seedApplication cria organização e application diretamente, sem passar pelo
// adminctl: este teste é do service de endpoints, não do bootstrap.
func seedApplication(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	orgID, err := ids.New()
	if err != nil {
		t.Fatalf("ids.New(): %v", err)
	}
	appID, err := ids.New()
	if err != nil {
		t.Fatalf("ids.New(): %v", err)
	}
	hasher, err := apikey.NewHasher(master)
	if err != nil {
		t.Fatalf("NewHasher(): %v", err)
	}
	_, hash, err := hasher.Generate()
	if err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	q := sqlc.New(pool)
	if _, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		ID: pgUUID(orgID), Name: "teste",
	}); err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if _, err := q.CreateApplication(ctx, sqlc.CreateApplicationParams{
		ID: pgUUID(appID), OrganizationID: pgUUID(orgID), Name: "teste",
		ApiKeyHash: hash, Locale: "pt-BR", BackoffProfile: "production",
	}); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	return appID
}

func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }

func TestCreateReturnsAUsableSecret(t *testing.T) {
	svc, appID, pool := newFixture(t)
	ctx := context.Background()

	got, err := svc.Create(ctx, appID, "https://api.cliente.com/hooks")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !strings.HasPrefix(got.Secret, "whsec_") {
		t.Errorf("secret = %q, queria prefixo whsec_", got.Secret)
	}
	if got.Status != "active" {
		t.Errorf("status = %q, want active", got.Status)
	}

	// O secret guardado, decifrado, tem de ser o que foi devolvido. Sem este
	// teste o service poderia devolver uma coisa e gravar outra, e só a spec
	// de disparo descobriria — como assinatura que o cliente rejeita.
	var blob []byte
	err = pool.QueryRow(ctx,
		`SELECT secret_encrypted FROM endpoints WHERE id = $1`, pgUUID(got.ID)).Scan(&blob)
	if err != nil {
		t.Fatalf("ler secret_encrypted: %v", err)
	}
	if strings.Contains(string(blob), got.Secret) {
		t.Fatal("o secret está em claro dentro da coluna")
	}
	cipher, err := secrets.NewCipher(master)
	if err != nil {
		t.Fatalf("NewCipher(): %v", err)
	}
	plain, err := cipher.Open(blob, []byte(ids.Encode(ids.Endpoint, got.ID)))
	if err != nil {
		t.Fatalf("Open() error = %v — o AAD gravado não é o id do endpoint", err)
	}
	if string(plain) != got.Secret {
		t.Errorf("secret decifrado = %q, queria %q", plain, got.Secret)
	}
}

// O teste central desta task. Sem a trava dentro da transação, N criações
// simultâneas leem a mesma contagem e todas passam — e nada dá sintoma.
func TestConcurrentCreatesStopAtThePlanLimit(t *testing.T) {
	svc, appID, _ := newFixture(t)
	ctx := context.Background()

	const attempts = 6
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.Create(ctx, appID, "https://api.cliente.com/hooks/"+string(rune('a'+i)))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	created, refused := 0, 0
	for err := range results {
		switch {
		case err == nil:
			created++
		case errors.Is(err, errs.EndpointLimit):
			refused++
		default:
			t.Errorf("erro inesperado: %v", err)
		}
	}
	if created != 2 {
		t.Errorf("criados = %d, queria exatamente 2", created)
	}
	if refused != attempts-2 {
		t.Errorf("recusados = %d, queria %d", refused, attempts-2)
	}
}

func TestCreateRefusesADuplicateURL(t *testing.T) {
	svc, appID, _ := newFixture(t)
	ctx := context.Background()
	const url = "https://api.cliente.com/hooks"

	if _, err := svc.Create(ctx, appID, url); err != nil {
		t.Fatalf("primeiro Create() error = %v", err)
	}
	if _, err := svc.Create(ctx, appID, url); !errors.Is(err, errs.DuplicateEndpoint) {
		t.Errorf("error = %v, queria errs.DuplicateEndpoint", err)
	}
}

func TestCreateRejectsBadURLsWithoutWriting(t *testing.T) {
	svc, appID, pool := newFixture(t)
	ctx := context.Background()

	for _, tt := range []struct {
		name string
		url  string
		want error
	}{
		{"http", "http://api.cliente.com/hooks", errs.InvalidEndpointURL},
		{"faixa proibida", "https://interno.exemplo.com/hooks", errs.ForbiddenAddress},
		{"não resolve", "https://naoexiste.exemplo.com/hooks", errs.UnresolvableHost},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.Create(ctx, appID, tt.url); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, queria %v", err, tt.want)
			}
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM endpoints`).Scan(&n); err != nil {
				t.Fatalf("contar: %v", err)
			}
			if n != 0 {
				t.Errorf("endpoints = %d, a validação devia vir antes da escrita", n)
			}
		})
	}
}

func TestCreateRefusesAnUnknownApplication(t *testing.T) {
	svc, _, _ := newFixture(t)
	other, err := ids.New()
	if err != nil {
		t.Fatalf("ids.New(): %v", err)
	}
	if _, err := svc.Create(context.Background(), other, "https://api.cliente.com/hooks"); !errors.Is(err, errs.ApplicationNotFound) {
		t.Errorf("error = %v, queria errs.ApplicationNotFound", err)
	}
}

func TestListDoesNotCarryTheSecret(t *testing.T) {
	svc, appID, _ := newFixture(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, appID, "https://api.cliente.com/hooks"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got, err := svc.List(ctx, appID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Secret != "" {
		t.Error("a listagem trouxe o secret")
	}
}

func TestGetCarriesTheSecret(t *testing.T) {
	svc, appID, _ := newFixture(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, appID, "https://api.cliente.com/hooks")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got, err := svc.Get(ctx, appID, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Secret != created.Secret {
		t.Errorf("secret = %q, queria %q", got.Secret, created.Secret)
	}
}

// Endpoint de outro tenant é indistinguível de inexistente.
func TestGetFromAnotherApplicationIsNotFound(t *testing.T) {
	svc, appID, pool := newFixture(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, appID, "https://api.cliente.com/hooks")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	otherApp := seedApplication(t, ctx, pool)

	if _, err := svc.Get(ctx, otherApp, created.ID); !errors.Is(err, errs.EndpointNotFound) {
		t.Errorf("error = %v, queria errs.EndpointNotFound", err)
	}
}

func TestUpdateURLChangesTheURLAndKeepsTheSecret(t *testing.T) {
	svc, appID, _ := newFixture(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, appID, "https://api.cliente.com/hooks")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	updated, err := svc.UpdateURL(ctx, appID, created.ID, "https://api.cliente.com/v2/hooks")
	if err != nil {
		t.Fatalf("UpdateURL() error = %v", err)
	}
	if updated.URL != "https://api.cliente.com/v2/hooks" {
		t.Errorf("url = %q", updated.URL)
	}

	// O secret não muda: é o motivo de o PATCH existir.
	again, err := svc.Get(ctx, appID, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if again.Secret != created.Secret {
		t.Error("o PATCH trocou o secret")
	}
}

func TestUpdateURLRejectsABadURLWithoutChangingAnything(t *testing.T) {
	svc, appID, _ := newFixture(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, appID, "https://api.cliente.com/hooks")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := svc.UpdateURL(ctx, appID, created.ID, "http://api.cliente.com/hooks"); !errors.Is(err, errs.InvalidEndpointURL) {
		t.Fatalf("error = %v, queria errs.InvalidEndpointURL", err)
	}

	again, err := svc.Get(ctx, appID, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if again.URL != "https://api.cliente.com/hooks" {
		t.Errorf("url = %q, a URL antiga devia ter sido preservada", again.URL)
	}
}
