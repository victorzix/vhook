package obs_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/victorzix/vhook/internal/obs"
)

func ok(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

func TestCorrelationGeneratesWhenClientSendsNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	obs.Correlation(http.HandlerFunc(ok)).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	got := rec.Header().Get(obs.HeaderCorrelationID)
	if len(got) != 26 {
		t.Errorf("correlation id = %q, queria 26 caracteres de base32", got)
	}
}

func TestCorrelationIsReadableFromTheContext(t *testing.T) {
	var fromContext string
	handler := obs.Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fromContext = obs.CorrelationID(r.Context())
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if fromContext == "" {
		t.Fatal("contexto não carregou o correlation id")
	}
	if fromContext != rec.Header().Get(obs.HeaderCorrelationID) {
		t.Error("o id do contexto difere do id do header")
	}
}

func TestCorrelationNeverAdoptsTheClientValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set(obs.HeaderClientCorrelationID, "cliente-123")
	rec := httptest.NewRecorder()

	obs.Correlation(http.HandlerFunc(ok)).ServeHTTP(rec, req)

	// O nosso é sempre nosso: se o valor do cliente virasse o de rastreio,
	// duas requisições distintas poderiam colidir na investigação.
	if got := rec.Header().Get(obs.HeaderCorrelationID); got == "cliente-123" {
		t.Error("o valor do cliente foi adotado como correlation id")
	}
}

func TestCorrelationGeneratesUniqueIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		rec := httptest.NewRecorder()
		obs.Correlation(http.HandlerFunc(ok)).
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		id := rec.Header().Get(obs.HeaderCorrelationID)
		if seen[id] {
			t.Fatalf("correlation id repetido: %s", id)
		}
		seen[id] = true
	}
}

func TestRecoverTurnsPanicIntoAnErrorEnvelope(t *testing.T) {
	var logged strings.Builder
	logger := obs.NewLogger(&logged, slog.LevelInfo)

	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("segredo que não pode vazar")
	})
	handler := obs.Correlation(obs.Recover(logger)(boom))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	var body struct {
		Error struct {
			Code          string `json:"code"`
			CorrelationID string `json:"correlation_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo não é o envelope de erro: %v", err)
	}
	if body.Error.Code != "SYS-INT-001" {
		t.Errorf("code = %q, want SYS-INT-001", body.Error.Code)
	}
	if body.Error.CorrelationID == "" {
		t.Error("correlation_id vazio: sem ele não há como investigar")
	}

	// Nunca mensagem, nunca stack trace na resposta.
	if strings.Contains(rec.Body.String(), "segredo") {
		t.Error("o valor do panic vazou no corpo da resposta")
	}
	if strings.Contains(rec.Body.String(), "goroutine") {
		t.Error("stack trace vazou no corpo da resposta")
	}

	// Mas tudo isso precisa estar no log.
	if !strings.Contains(logged.String(), "segredo") {
		t.Error("o valor do panic não foi logado")
	}
}

func TestRecoverLetsHealthyRequestsThrough(t *testing.T) {
	logger := obs.NewLogger(io.Discard, slog.LevelInfo)
	rec := httptest.NewRecorder()
	obs.Recover(logger)(http.HandlerFunc(ok)).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRequestLogRecordsTheCorrelationID(t *testing.T) {
	var out strings.Builder
	handler := obs.Correlation(obs.RequestLog(obs.NewLogger(&out, slog.LevelInfo))(
		http.HandlerFunc(ok)))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var line map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &line); err != nil {
		t.Fatalf("log não é JSON: %v — %q", err, out.String())
	}
	if line["correlation_id"] != rec.Header().Get(obs.HeaderCorrelationID) {
		t.Errorf("correlation_id do log difere do header: %v", line["correlation_id"])
	}
	if line["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", line["status"])
	}
	if line["path"] != "/healthz" {
		t.Errorf("path = %v", line["path"])
	}
}

func TestRequestLogRecordsAValidClientCorrelationID(t *testing.T) {
	var out strings.Builder
	handler := obs.Correlation(obs.RequestLog(obs.NewLogger(&out, slog.LevelInfo))(
		http.HandlerFunc(ok)))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set(obs.HeaderClientCorrelationID, "cliente-123")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	var line map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &line); err != nil {
		t.Fatalf("log não é JSON: %v", err)
	}
	if line["client_correlation_id"] != "cliente-123" {
		t.Errorf("client_correlation_id = %v", line["client_correlation_id"])
	}
}

func TestInvalidClientHeaderIsDroppedWithoutFailingTheRequest(t *testing.T) {
	var out strings.Builder
	handler := obs.Correlation(obs.RequestLog(obs.NewLogger(&out, slog.LevelInfo))(
		http.HandlerFunc(ok)))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	// Quebra as duas regras de uma vez: comprimento e alfabeto.
	req.Header.Set(obs.HeaderClientCorrelationID, strings.Repeat("x", 70)+"\nINJETADO")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Recusar uma requisição por causa de um header de rastreio opcional
	// malformado seria hostil.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	var line map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &line); err != nil {
		t.Fatalf("log não é JSON: %v", err)
	}
	if line["client_correlation_id_dropped"] != true {
		t.Error("o valor inválido devia ter sido marcado como descartado")
	}
	if _, present := line["client_correlation_id"]; present {
		t.Error("valor controlado pelo cliente e inválido não pode ir para o log")
	}
}
