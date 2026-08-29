package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/victorzix/vhook/internal/apikey"
	"github.com/victorzix/vhook/internal/ids"
	"github.com/victorzix/vhook/internal/obs"
	"github.com/victorzix/vhook/internal/store"
	"github.com/victorzix/vhook/internal/store/sqlc"
)

const testAdminToken = "token-de-teste-do-management"

// testHost is on the SSRF allowlist below, so no test here depends on DNS
// answering for a domain we do not own. The forbidden cases use IP literals,
// which the resolver returns without touching the network.
const testHost = "api.exemplo.com"

var testMasterKey = []byte("0123456789abcdef0123456789abcdef")

// testConfig is the configuration both this file and server_test.go feed to
// the production wiring.
func testConfig(dbURL, amqpURL string) config {
	return config{
		databaseURL:   dbURL,
		rabbitURL:     amqpURL,
		httpAddr:      ":0",
		masterKey:     testMasterKey,
		adminToken:    testAdminToken,
		ssrfAllowlist: []string{testHost},
	}
}

// harness sobe Postgres, migra, semeia uma application e devolve o servidor
// HTTP montado com o router de produção, mais o id externo da application.
type harness struct {
	server *httptest.Server
	appID  string
	pool   *pgxpool.Pool
}

func newHarness(t *testing.T) harness {
	t.Helper()
	if testing.Short() {
		t.Skip("integração: precisa de Docker")
	}

	pool := startMigratedPostgres(t)
	appID := seedApplication(t, pool)

	logger := obs.NewLogger(io.Discard, slog.LevelError)
	health := obs.NewHealth(logger, postgresCheck(pool))

	// O mesmo buildRouter que o main.go chama: um router só de teste provaria
	// que o router de teste funciona, e não que a API está protegida.
	router, err := buildRouter(logger, health, pool, testConfig("", ""))
	if err != nil {
		t.Fatalf("buildRouter() error = %v", err)
	}

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return harness{server: server, appID: appID, pool: pool}
}

// startMigratedPostgres sobe o container, aplica as migrations e devolve o
// pool já aberto.
func startMigratedPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
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
	return pool
}

// seedApplication cria organização e application direto no banco: este teste
// é das rotas de endpoint, não do bootstrap.
func seedApplication(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()

	orgID, err := ids.New()
	if err != nil {
		t.Fatalf("ids.New(): %v", err)
	}
	appID, err := ids.New()
	if err != nil {
		t.Fatalf("ids.New(): %v", err)
	}
	hasher, err := apikey.NewHasher(testMasterKey)
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
	return ids.Encode(ids.Application, appID)
}

// seedApplicationIn cria uma segunda application no mesmo banco.
func seedApplicationIn(t *testing.T, h harness) string {
	t.Helper()
	return seedApplication(t, h.pool)
}

func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }

// do executa uma requisição já autenticada, salvo quando token == "".
func (h harness) do(t *testing.T, method, path, token, body string) *http.Response {
	t.Helper()
	var rdr io.Reader = http.NoBody
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, h.server.URL+path, rdr)
	if err != nil {
		t.Fatalf("montar requisição: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func bearer() string { return "Bearer " + testAdminToken }

func decodeCode(t *testing.T, res *http.Response) string {
	t.Helper()
	var body struct {
		Error struct {
			Code          string `json:"code"`
			CorrelationID string `json:"correlation_id"`
		} `json:"error"`
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ler corpo: %v", err)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("corpo não é o envelope de erro: %v — %s", err, raw)
	}
	// Sem correlation_id não há como investigar um caso relatado.
	if body.Error.CorrelationID == "" {
		t.Error("resposta de erro sem correlation_id")
	}
	// A resposta de erro nunca carrega mensagem: o dashboard traduz o código.
	if strings.Contains(string(raw), `"message"`) {
		t.Errorf("a resposta de erro carrega mensagem: %s", raw)
	}
	return body.Error.Code
}

func decodeEndpoint(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decodificar endpoint: %v", err)
	}
	return out
}

// A autenticação é a razão de o teste montar o router de produção: um
// middleware que não protege é indistinguível de um que protege, até alguém
// chamar sem token.
func TestManagementRoutesRefuseWithoutAValidToken(t *testing.T) {
	h := newHarness(t)
	path := "/v1/applications/" + h.appID + "/endpoints"

	for _, tt := range []struct{ name, token string }{
		{"sem header", ""},
		{"token errado", "Bearer errado"},
		{"esquema errado", "Basic " + testAdminToken},
		{"sem esquema", testAdminToken},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := h.do(t, http.MethodGet, path, tt.token, "")
			if res.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", res.StatusCode)
			}
			if got := decodeCode(t, res); got != "AUT-CRD-001" {
				t.Errorf("code = %q, want AUT-CRD-001", got)
			}
		})
	}
}

// As rotas operacionais são públicas por natureza, e o middleware que lê o
// contrato precisa continuar deixando-as passar sem credencial.
func TestOperationalRoutesStayPublic(t *testing.T) {
	h := newHarness(t)

	if res := h.do(t, http.MethodGet, "/healthz", "", ""); res.StatusCode != http.StatusOK {
		t.Errorf("/healthz = %d, want 200", res.StatusCode)
	}
}

func TestCreateReturnsTheSecretAndListDoesNot(t *testing.T) {
	h := newHarness(t)
	path := "/v1/applications/" + h.appID + "/endpoints"

	res := h.do(t, http.MethodPost, path, bearer(), `{"url":"https://`+testHost+`/hooks"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}
	created := decodeEndpoint(t, res)
	secret, _ := created["secret"].(string)
	if !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("secret = %q, queria prefixo whsec_", secret)
	}

	res = h.do(t, http.MethodGet, path, bearer(), "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ler corpo: %v", err)
	}
	if strings.Contains(string(raw), secret) {
		t.Error("a listagem trouxe o secret")
	}

	id, _ := created["id"].(string)
	res = h.do(t, http.MethodGet, path+"/"+id, bearer(), "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if got, _ := decodeEndpoint(t, res)["secret"].(string); got != secret {
		t.Error("o detalhe devolveu secret diferente do da criação")
	}
}

func TestCreateRefusesBeyondThePlanLimit(t *testing.T) {
	h := newHarness(t)
	path := "/v1/applications/" + h.appID + "/endpoints"

	for _, suffix := range []string{"a", "b"} {
		res := h.do(t, http.MethodPost, path, bearer(),
			`{"url":"https://`+testHost+`/hooks/`+suffix+`"}`)
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d, want 201", res.StatusCode)
		}
	}

	res := h.do(t, http.MethodPost, path, bearer(), `{"url":"https://`+testHost+`/hooks/c"}`)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — 429 prometeria que esperar resolve", res.StatusCode)
	}
	if got := decodeCode(t, res); got != "RTL-LMT-001" {
		t.Errorf("code = %q, want RTL-LMT-001", got)
	}
}

func TestCreateRefusesADuplicateURL(t *testing.T) {
	h := newHarness(t)
	path := "/v1/applications/" + h.appID + "/endpoints"
	body := `{"url":"https://` + testHost + `/hooks"}`

	if res := h.do(t, http.MethodPost, path, bearer(), body); res.StatusCode != http.StatusCreated {
		t.Fatalf("primeiro POST: status = %d, want 201", res.StatusCode)
	}
	res := h.do(t, http.MethodPost, path, bearer(), body)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", res.StatusCode)
	}
	if got := decodeCode(t, res); got != "EPT-CFL-001" {
		t.Errorf("code = %q, want EPT-CFL-001", got)
	}
}

func TestCreateRejectsForbiddenDestinations(t *testing.T) {
	h := newHarness(t)
	path := "/v1/applications/" + h.appID + "/endpoints"

	for _, tt := range []struct{ name, url, code string }{
		{"http", "http://" + testHost + "/hooks", "EPT-VAL-001"},
		{"metadados de cloud", "https://169.254.169.254/latest/meta-data/", "EPT-VAL-002"},
		{"rede privada", "https://10.0.0.1/hooks", "EPT-VAL-002"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := h.do(t, http.MethodPost, path, bearer(), `{"url":"`+tt.url+`"}`)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", res.StatusCode)
			}
			if got := decodeCode(t, res); got != tt.code {
				t.Errorf("code = %q, want %q", got, tt.code)
			}
		})
	}
}

func TestMalformedIdentifierIsUnprocessable(t *testing.T) {
	h := newHarness(t)
	res := h.do(t, http.MethodGet, "/v1/applications/app_naoehumid/endpoints", bearer(), "")
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
	if got := decodeCode(t, res); got != "SYS-VAL-001" {
		t.Errorf("code = %q, want SYS-VAL-001", got)
	}
}

// Recurso de outro tenant é indistinguível de inexistente: um 403 confirmaria
// que ele existe.
func TestEndpointOfAnotherApplicationIsNotFound(t *testing.T) {
	h := newHarness(t)
	path := "/v1/applications/" + h.appID + "/endpoints"

	res := h.do(t, http.MethodPost, path, bearer(), `{"url":"https://`+testHost+`/hooks"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}
	id, _ := decodeEndpoint(t, res)["id"].(string)

	other := seedApplicationIn(t, h)
	res = h.do(t, http.MethodGet, "/v1/applications/"+other+"/endpoints/"+id, bearer(), "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.StatusCode)
	}
	if got := decodeCode(t, res); got != "EPT-NFD-001" {
		t.Errorf("code = %q, want EPT-NFD-001", got)
	}
}

func TestPatchChangesTheURLAndKeepsTheSecret(t *testing.T) {
	h := newHarness(t)
	path := "/v1/applications/" + h.appID + "/endpoints"

	res := h.do(t, http.MethodPost, path, bearer(), `{"url":"https://`+testHost+`/hooks"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}
	created := decodeEndpoint(t, res)
	id, _ := created["id"].(string)
	secret, _ := created["secret"].(string)

	res = h.do(t, http.MethodPatch, path+"/"+id, bearer(), `{"url":"https://`+testHost+`/v2/hooks"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	patched := decodeEndpoint(t, res)
	if patched["url"] != "https://"+testHost+"/v2/hooks" {
		t.Errorf("url = %v", patched["url"])
	}
	if _, present := patched["secret"]; present {
		t.Error("a resposta do PATCH trouxe o secret")
	}

	// O secret sobreviver é o motivo de o PATCH existir: sem ele, corrigir um
	// typo obrigaria o cliente a se reconfigurar.
	res = h.do(t, http.MethodGet, path+"/"+id, bearer(), "")
	if got, _ := decodeEndpoint(t, res)["secret"].(string); got != secret {
		t.Error("o PATCH trocou o secret")
	}
}
