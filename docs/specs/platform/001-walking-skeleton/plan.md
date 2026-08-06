# Walking skeleton — Plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Subir a `api` do vhook com schema aplicado, contrato gerando tipos, registro de erros e os três endpoints operacionais, de forma que toda spec seguinte só precise escrever a sua feature.

**Architecture:** Um único binário Go (`cmd/api`) montando um router `chi`. Postgres e RabbitMQ sobem por `docker compose`; a `api` roda na máquina do desenvolvedor via `make run`. As migrations são embutidas no binário e aplicadas no boot sob advisory lock. Tipos vêm de duas fontes geradas — `sqlc` a partir do schema, `oapi-codegen` a partir de `contracts/openapi.yaml` — e nunca são editados à mão.

**Tech Stack:** Go 1.24 · `go-chi/chi/v5` · `jackc/pgx/v5` · `golang-migrate/migrate/v4` · `rabbitmq/amqp091-go` · `google/uuid` · `prometheus/client_golang` · `sqlc` · `oapi-codegen` · `testcontainers-go` · Postgres 17 · RabbitMQ 4

**Spec:** [`spec.md`](spec.md) · **Release alvo:** `v0.1.0`

## Global Constraints

Herdadas por toda task. Não repetir dentro delas.

- TDD estrito: nenhum código de produção sem teste que falhou primeiro. Ver skill `test-driven-development`.
- `internal/core` não importa Postgres, Rabbit nem `net/http`.
- Timeout de 5s por tentativa de entrega; `io.LimitReader` de 64KB na resposta.
- 4xx é falha permanente, exceto 408 e 429.
- Payload trafega como `[]byte` cru — nunca desserializar antes de assinar.
- Ack da mensagem original só depois do confirm do publish.
- DLQ por publicação explícita; nunca dead-letter na fila `deliveries`.
- Número de shards é constante em código, idêntica em `api`, `worker` e `reconciler`.
- Nenhuma métrica Prometheus com label `application_id`.
- Paginação por cursor keyset; nunca `OFFSET`.
- Env só para segredo e endereço; comportamento em código; por tenant no banco.
- Payload e headers de assinatura nunca em log.
- Documentação e commits em português; código, identificadores e logs em inglês.

**Específico desta release:**

- Module path: `github.com/victorzix/vhook`.
- Teste de integração é marcado com `if testing.Short() { t.Skip(...) }`. `make test` usa `-short`; `make test-integration` e o CI rodam tudo.
- **Quem commita é o dono do repositório.** Os passos de commit deste plano entregam a mensagem pronta; não rode `git commit`.
- Arquivos e pastas em inglês; conteúdo de documentação em português.

---

## Estrutura de arquivos

| Arquivo | Responsabilidade |
|---|---|
| `go.mod` | módulo e ferramentas de codegen fixadas por `tool` |
| `docker-compose.yml` | `postgres`, `rabbitmq`, `api` — local sobe só os dois primeiros |
| `Dockerfile` | build multi-stage do binário `api` |
| `Makefile` | `up` `down` `run` `generate` `test` `test-integration` |
| `.env.example` | as três variáveis de ambiente |
| `migrations/migrations.go` | `embed.FS` dos `.sql` — precisa ser pacote para o `go:embed` alcançar |
| `migrations/000001_initial_schema.{up,down}.sql` | as 7 tabelas e seus índices |
| `migrations/000002_vhook_id_function.{up,down}.sql` | `vhook_id(text) → uuid` |
| `i18n/i18n.go` | `embed.FS` dos catálogos e a lista canônica de locales |
| `i18n/errors.{pt-BR,en,es,fr}.json` | código → mensagem, sem comportamento |
| `internal/ids/ids.go` | UUIDv7 ↔ base32 Crockford, com e sem prefixo |
| `internal/ids/testdata/vectors.json` | vetores fixos, compartilhados com o teste da função SQL |
| `internal/errs/errs.go` | tipos, registro e as 5 constantes |
| `internal/store/migrate.go` | runner de migration sob advisory lock |
| `internal/store/pool.go` | construção do `pgxpool` |
| `internal/store/queries/health.sql` | a query de health, entrada do `sqlc` |
| `internal/store/sqlc/` | **gerado** pelo `sqlc` |
| `internal/openapi/openapi.gen.go` | **gerado** pelo `oapi-codegen` |
| `internal/obs/log.go` | `slog` JSON e correlation id no contexto |
| `internal/obs/middleware.go` | middlewares de correlation e de recover |
| `internal/obs/httperr.go` | serialização do envelope de erro |
| `internal/obs/health.go` | handlers de `/healthz`, `/readyz` e `/metrics` |
| `cmd/api/config.go` | leitura e validação do ambiente |
| `cmd/api/main.go` | wiring, migrations no boot, desligamento gracioso |

**Por que `migrations/` e `i18n/` viram pacotes Go.** `go:embed` só alcança arquivos abaixo do diretório do pacote que o declara. Um `//go:embed ../../migrations` em `internal/store` não compila. A saída é dar a esses diretórios um arquivo `.go` mínimo cujo único trabalho é expor o `embed.FS`.

---

## Task 1: Bootstrap do módulo, do compose e do Makefile

Não há teste automatizado aqui: a entrega é andaime, e a verificação é o serviço subir. Toda task seguinte tem ciclo red-green.

**Files:**
- Create: `go.mod`
- Create: `docker-compose.yml`
- Create: `Makefile`
- Create: `.env.example`

**Interfaces:**
- Consumes: nada.
- Produces: module path `github.com/victorzix/vhook`; alvos `make up`, `make down`, `make test`, `make test-integration`; Postgres em `localhost:5432` com usuário/senha/base `vhook`; RabbitMQ em `localhost:5672` com `guest:guest`.

- [ ] **Step 1: Inicializar o módulo**

```bash
go mod init github.com/victorzix/vhook
```

Edite `go.mod` para fixar a versão da linguagem:

```
module github.com/victorzix/vhook

go 1.24
```

- [ ] **Step 2: Escrever o `docker-compose.yml`**

O serviço `api` **não** entra agora — ele depende do `Dockerfile` e do binário, que só existem na Task 9. Subir um compose que não builda seria deixar o repositório quebrado por oito tasks.

```yaml
name: vhook

services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: vhook
      POSTGRES_PASSWORD: vhook
      POSTGRES_DB: vhook
    ports:
      - "5432:5432"
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U vhook -d vhook"]
      interval: 5s
      timeout: 3s
      retries: 10

  rabbitmq:
    image: rabbitmq:4-management-alpine
    ports:
      - "5672:5672"
      - "15672:15672"
    volumes:
      - rabbitmq-data:/var/lib/rabbitmq
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "-q", "check_running"]
      interval: 5s
      timeout: 5s
      retries: 10

volumes:
  postgres-data:
  rabbitmq-data:
```

- [ ] **Step 3: Escrever o `Makefile`**

**As linhas de receita usam TAB, não espaços** — `make` falha com "missing separator" se forem espaços.

```make
.PHONY: up down run generate test test-integration

## up: sobe só a infraestrutura; a api roda local com `make run`
up:
	docker compose up -d postgres rabbitmq

down:
	docker compose down

run:
	go run ./cmd/api

generate:
	go tool sqlc generate
	go tool oapi-codegen -config contracts/oapi-codegen.yaml contracts/openapi.yaml

## test: só unidade — rápido o bastante para rodar a cada green
test:
	go test -race -short ./...

## test-integration: sobe container de verdade; é o que o CI roda
test-integration:
	go test -race -shuffle=on ./...
```

O alvo `generate` só funciona a partir da Task 6, quando as ferramentas e os arquivos de configuração existirem. Ele nasce agora porque o CI procura por `^generate:` no `Makefile` e sai limpo enquanto não encontra — deixar para depois manteria esse job em silêncio.

- [ ] **Step 4: Escrever o `.env.example`**

```
# Endereços de infraestrutura. Nenhum comportamento do sistema mora aqui.
DATABASE_URL=postgres://vhook:vhook@localhost:5432/vhook?sslmode=disable
RABBITMQ_URL=amqp://guest:guest@localhost:5672/
VHOOK_HTTP_ADDR=:8080
```

`.env` já está no `.gitignore`, com exceção explícita para `.env.example`.

- [ ] **Step 5: Verificar que a infraestrutura sobe**

```bash
make up
docker compose ps
```

Esperado: `postgres` e `rabbitmq` com estado `healthy`. Se o Postgres reclamar de porta ocupada, algo já está usando a 5432 — pare o outro serviço em vez de trocar a porta, porque o `.env.example` combina com esta.

- [ ] **Step 6: Verificar que o módulo compila**

```bash
go build ./...
```

Esperado: sem saída (não há pacote ainda, e isso não é erro).

- [ ] **Step 7: Commit**

```bash
git add go.mod docker-compose.yml Makefile .env.example
```

Mensagem:

```
chore: iniciar módulo Go e infraestrutura local
```

---

## Task 2: `internal/ids` — UUIDv7 e base32 Crockford

**Files:**
- Create: `internal/ids/testdata/vectors.json`
- Create: `internal/ids/ids_test.go`
- Create: `internal/ids/ids.go`

**Interfaces:**
- Consumes: nada.
- Produces:
  - `type Prefix string` e as constantes `Organization`, `Application`, `Endpoint`, `Event`, `Delivery`, `DeliveryAttempt`
  - `func New() (uuid.UUID, error)`
  - `func Render(id uuid.UUID) string` — base32 sem prefixo, usado pelo correlation id
  - `func Encode(p Prefix, id uuid.UUID) string` — `"<prefix>_<base32>"`
  - `func Parse(p Prefix, s string) (uuid.UUID, error)`
  - `var ErrMalformed, ErrWrongPrefix error`

- [ ] **Step 1: Adicionar a dependência de UUID**

```bash
go get github.com/google/uuid@latest
```

- [ ] **Step 2: Escrever os vetores fixos**

Os quatro valores foram conferidos contra uma implementação independente de base32 Crockford. **Não recalcule à mão** — um vetor errado transformaria o teste em confirmação do bug.

`internal/ids/testdata/vectors.json`:

```json
[
  {
    "name": "zero",
    "uuid": "00000000-0000-0000-0000-000000000000",
    "base32": "00000000000000000000000000"
  },
  {
    "name": "max",
    "uuid": "ffffffff-ffff-ffff-ffff-ffffffffffff",
    "base32": "7ZZZZZZZZZZZZZZZZZZZZZZZZZ"
  },
  {
    "name": "v7-may-2024",
    "uuid": "018f4c2a-7b31-7c4e-9a2b-1f5c8d3e6b04",
    "base32": "01HX62MYSHFH79MARZBJ6KWTR4"
  },
  {
    "name": "v7-july-2024",
    "uuid": "01912d4e-8f00-7000-8000-000000000001",
    "base32": "01J4PMX3R0E008000000000001"
  }
]
```

- [ ] **Step 3: Escrever os testes que falham**

`internal/ids/ids_test.go`:

```go
package ids_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/victorzix/vhook/internal/ids"
)

type vector struct {
	Name   string `json:"name"`
	UUID   string `json:"uuid"`
	Base32 string `json:"base32"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	raw, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vs []vector
	if err := json.Unmarshal(raw, &vs); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if len(vs) == 0 {
		t.Fatal("no vectors")
	}
	return vs
}

func TestRenderMatchesVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			id := uuid.MustParse(v.UUID)
			if got := ids.Render(id); got != v.Base32 {
				t.Errorf("Render() = %q, want %q", got, v.Base32)
			}
		})
	}
}

func TestParseMatchesVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			got, err := ids.Parse(ids.Event, "evt_"+v.Base32)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got != uuid.MustParse(v.UUID) {
				t.Errorf("Parse() = %v, want %v", got, v.UUID)
			}
		})
	}
}

func TestEncodeAddsPrefix(t *testing.T) {
	id := uuid.MustParse("018f4c2a-7b31-7c4e-9a2b-1f5c8d3e6b04")
	want := "dlv_01HX62MYSHFH79MARZBJ6KWTR4"
	if got := ids.Encode(ids.Delivery, id); got != want {
		t.Errorf("Encode() = %q, want %q", got, want)
	}
}

func TestParseAcceptsLowercase(t *testing.T) {
	got, err := ids.Parse(ids.Event, "evt_01hx62myshfh79marzbj6kwtr4")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if want := uuid.MustParse("018f4c2a-7b31-7c4e-9a2b-1f5c8d3e6b04"); got != want {
		t.Errorf("Parse() = %v, want %v", got, want)
	}
}

func TestParseAcceptsAmbiguousCrockford(t *testing.T) {
	// I e L valem 1, O vale 0 — é o ponto do alfabeto Crockford.
	got, err := ids.Parse(ids.Event, "evt_OIHX62MYSHFH79MARZBJ6KWTR4")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if want := uuid.MustParse("018f4c2a-7b31-7c4e-9a2b-1f5c8d3e6b04"); got != want {
		t.Errorf("Parse() = %v, want %v", got, want)
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{"prefixo de outro recurso", "dlv_01HX62MYSHFH79MARZBJ6KWTR4", ids.ErrWrongPrefix},
		{"sem prefixo", "01HX62MYSHFH79MARZBJ6KWTR4", ids.ErrWrongPrefix},
		{"curto demais", "evt_01HX62MYSHFH79MARZBJ6KWTR", ids.ErrMalformed},
		{"longo demais", "evt_01HX62MYSHFH79MARZBJ6KWTR44", ids.ErrMalformed},
		{"U não pertence ao alfabeto", "evt_01HX62MYSHFH79MARZBJ6KWTU4", ids.ErrMalformed},
		{"estouro de 128 bits", "evt_8ZZZZZZZZZZZZZZZZZZZZZZZZZ", ids.ErrMalformed},
		{"vazio", "", ids.ErrWrongPrefix},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ids.Parse(ids.Event, tt.input); !errorsIs(err, tt.want) {
				t.Errorf("Parse() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNewProducesVersion7(t *testing.T) {
	id, err := ids.New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if id.Version() != 7 {
		t.Errorf("Version() = %d, want 7", id.Version())
	}
}

func TestRenderPreservesTimeOrder(t *testing.T) {
	early := ids.Render(uuid.MustParse("018f4c2a-7b31-7c4e-9a2b-1f5c8d3e6b04"))
	late := ids.Render(uuid.MustParse("01912d4e-8f00-7000-8000-000000000001"))
	if !(early < late) {
		t.Errorf("ordem lexicográfica quebrou: %q não é menor que %q", early, late)
	}
}
```

Adicione ao fim do arquivo o utilitário usado acima:

```go
func errorsIs(got, want error) bool {
	return got != nil && want != nil && errors.Is(got, want)
}
```

E o import de `"errors"` no bloco de imports.

- [ ] **Step 4: Rodar os testes para ver falhar**

```bash
go test ./internal/ids/ -v
```

Esperado: FAIL na compilação, `undefined: ids.Render`, `undefined: ids.Parse` e assim por diante.

- [ ] **Step 5: Implementar**

`internal/ids/ids.go`:

```go
// Package ids converts between the UUIDv7 stored in Postgres and the external
// form used by the API: a three-letter resource prefix followed by the same
// 128 bits in Crockford base32.
//
// See ARCHITECTURE.md §4.31.
package ids

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

// Prefix identifies the resource an external identifier belongs to. Pasting a
// delivery id where an endpoint id is expected becomes a named validation
// error instead of a puzzling 404.
type Prefix string

const (
	Organization    Prefix = "org"
	Application     Prefix = "app"
	Endpoint        Prefix = "ept"
	Event           Prefix = "evt"
	Delivery        Prefix = "dlv"
	DeliveryAttempt Prefix = "att"
)

var (
	// ErrMalformed means the base32 body is not a valid 128-bit identifier.
	ErrMalformed = errors.New("ids: malformed identifier")
	// ErrWrongPrefix means the identifier belongs to another resource.
	ErrWrongPrefix = errors.New("ids: wrong resource prefix")
)

const (
	alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	encoded  = 26 // 128 bits in 5-bit groups, rounded up
)

// New returns a fresh UUIDv7. Identifiers are generated by the application and
// never by the database: the ingress needs the delivery id before the insert,
// because that id is what goes on the queue.
func New() (uuid.UUID, error) {
	return uuid.NewV7()
}

// Render returns the base32 body with no prefix. Correlation ids use this:
// they are not resources, so they carry no prefix.
func Render(id uuid.UUID) string {
	v := [16]byte(id)
	out := make([]byte, encoded)
	for i := encoded - 1; i >= 0; i-- {
		out[i] = alphabet[v[15]&0x1f]
		shiftRight5(&v)
	}
	return string(out)
}

// Encode returns the external form of id for resource p.
func Encode(p Prefix, id uuid.UUID) string {
	return string(p) + "_" + Render(id)
}

// Parse decodes an external identifier, requiring it to carry prefix p.
// Input is case-insensitive and tolerates the ambiguous Crockford letters:
// I and L read as 1, O reads as 0.
func Parse(p Prefix, s string) (uuid.UUID, error) {
	want := string(p) + "_"
	if !strings.HasPrefix(s, want) {
		return uuid.Nil, ErrWrongPrefix
	}
	body := s[len(want):]
	if len(body) != encoded {
		return uuid.Nil, ErrMalformed
	}

	var v [16]byte
	for i := 0; i < encoded; i++ {
		d, ok := decodeChar(body[i])
		if !ok {
			return uuid.Nil, ErrMalformed
		}
		if overflow := shiftLeft5(&v, d); overflow {
			return uuid.Nil, ErrMalformed
		}
	}
	return uuid.UUID(v), nil
}

// shiftRight5 divides the big-endian 128-bit value by 32.
func shiftRight5(v *[16]byte) {
	var carry byte
	for i := 0; i < 16; i++ {
		cur := v[i]
		v[i] = cur>>5 | carry<<3
		carry = cur & 0x1f
	}
}

// shiftLeft5 multiplies the big-endian 128-bit value by 32 and adds add.
// It reports whether the result would exceed 128 bits.
func shiftLeft5(v *[16]byte, add byte) bool {
	if v[0]>>3 != 0 {
		return true
	}
	for i := 0; i < 15; i++ {
		v[i] = v[i]<<5 | v[i+1]>>3
	}
	v[15] = v[15]<<5 | add
	return false
}

func decodeChar(c byte) (byte, bool) {
	switch {
	case c >= 'a' && c <= 'z':
		c -= 'a' - 'A'
	}
	switch c {
	case 'I', 'L':
		return 1, true
	case 'O':
		return 0, true
	}
	if i := strings.IndexByte(alphabet, c); i >= 0 {
		return byte(i), true
	}
	return 0, false
}
```

- [ ] **Step 6: Rodar os testes para ver passar**

```bash
go test ./internal/ids/ -v
```

Esperado: PASS em todos, incluindo os quatro subtestes de vetor.

- [ ] **Step 7: Commit**

```bash
git add internal/ids go.mod go.sum
```

Mensagem:

```
feat: codificar identificadores uuidv7 com prefixo
```

---

## Task 3: `internal/errs` e o catálogo i18n

**Files:**
- Create: `i18n/i18n.go`
- Create: `i18n/errors.pt-BR.json`, `i18n/errors.en.json`, `i18n/errors.es.json`, `i18n/errors.fr.json`
- Create: `internal/errs/errs_test.go`
- Create: `internal/errs/catalog_test.go`
- Create: `internal/errs/errs.go`

**Interfaces:**
- Consumes: nada.
- Produces:
  - `type Level string` com `LevelWarn` e `LevelError`
  - `type Error struct { Code string; Level Level; HTTPStatus int }`, com `func (e *Error) Error() string`
  - As constantes `StorageUnavailable`, `QueueUnavailable`, `Draining`, `Internal`, `MissingConfig`
  - `func All() []*Error` — o registro completo, usado pelo teste de completude
  - `i18n.Locales []string` e `i18n.Load(locale string) (map[string]string, error)`

- [ ] **Step 1: Escrever os catálogos**

Todos os cinco códigos em todos os quatro arquivos. Chaves em ordem alfabética para que uma ausência salte aos olhos num diff.

`i18n/errors.pt-BR.json`:

```json
{
  "CFG-VAL-001": "Configuração obrigatória ausente.",
  "QUE-DEP-001": "Fila de mensagens indisponível.",
  "STO-DEP-001": "Banco de dados indisponível.",
  "SYS-DEP-001": "Serviço em desligamento. Tente novamente.",
  "SYS-INT-001": "Falha interna inesperada."
}
```

`i18n/errors.en.json`:

```json
{
  "CFG-VAL-001": "Required configuration is missing.",
  "QUE-DEP-001": "Message queue unavailable.",
  "STO-DEP-001": "Database unavailable.",
  "SYS-DEP-001": "Service is shutting down. Try again.",
  "SYS-INT-001": "Unexpected internal failure."
}
```

`i18n/errors.es.json`:

```json
{
  "CFG-VAL-001": "Falta configuración obligatoria.",
  "QUE-DEP-001": "Cola de mensajes no disponible.",
  "STO-DEP-001": "Base de datos no disponible.",
  "SYS-DEP-001": "El servicio se está apagando. Inténtelo de nuevo.",
  "SYS-INT-001": "Fallo interno inesperado."
}
```

`i18n/errors.fr.json`:

```json
{
  "CFG-VAL-001": "Configuration obligatoire manquante.",
  "QUE-DEP-001": "File de messages indisponible.",
  "STO-DEP-001": "Base de données indisponible.",
  "SYS-DEP-001": "Service en cours d'arrêt. Réessayez.",
  "SYS-INT-001": "Échec interne inattendu."
}
```

- [ ] **Step 2: Expor os catálogos como pacote Go**

`go:embed` não alcança diretório acima do pacote, então `i18n/` precisa ser um pacote.

`i18n/i18n.go`:

```go
// Package i18n embeds the error message catalogue. The catalogue has no
// behaviour and the registry in internal/errs has no text: keeping them apart
// is what stops them from drifting. See ARCHITECTURE.md §4.29.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed errors.*.json
var files embed.FS

// Locales is the canonical list. The dashboard serves these four, and it is
// the dashboard that translates the API's error codes.
var Locales = []string{"pt-BR", "en", "es", "fr"}

// Default is the fallback and the default of applications.locale.
const Default = "pt-BR"

// Load returns the code-to-message map for a locale.
func Load(locale string) (map[string]string, error) {
	raw, err := files.ReadFile("errors." + locale + ".json")
	if err != nil {
		return nil, fmt.Errorf("i18n: unknown locale %q: %w", locale, err)
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("i18n: parse %q: %w", locale, err)
	}
	return out, nil
}
```

- [ ] **Step 3: Escrever os testes que falham**

`internal/errs/errs_test.go`:

```go
package errs_test

import (
	"errors"
	"net/http"
	"regexp"
	"testing"

	"github.com/victorzix/vhook/internal/errs"
)

var codeFormat = regexp.MustCompile(`^[A-Z]{3}-[A-Z]{3}-[0-9]{3}$`)

func TestEveryCodeMatchesTheFormat(t *testing.T) {
	for _, e := range errs.All() {
		if !codeFormat.MatchString(e.Code) {
			t.Errorf("código %q não casa com MOD-TYP-NNN", e.Code)
		}
	}
}

func TestNoDuplicateCodes(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range errs.All() {
		if seen[e.Code] {
			t.Errorf("código duplicado: %s", e.Code)
		}
		seen[e.Code] = true
	}
}

func TestEveryErrorHasALevel(t *testing.T) {
	for _, e := range errs.All() {
		if e.Level != errs.LevelWarn && e.Level != errs.LevelError {
			t.Errorf("%s: nível inválido %q", e.Code, e.Level)
		}
	}
}

func TestDeclaredOverrides(t *testing.T) {
	// As sobrescritas da spec 001. Se alguma mudar sem passar pela spec,
	// este teste é onde isso aparece.
	tests := []struct {
		err    *errs.Error
		level  errs.Level
		status int
	}{
		{errs.StorageUnavailable, errs.LevelError, http.StatusServiceUnavailable},
		{errs.QueueUnavailable, errs.LevelError, http.StatusServiceUnavailable},
		{errs.Draining, errs.LevelWarn, http.StatusServiceUnavailable},
		{errs.Internal, errs.LevelError, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.err.Code, func(t *testing.T) {
			if tt.err.Level != tt.level {
				t.Errorf("Level = %q, want %q", tt.err.Level, tt.level)
			}
			if tt.err.HTTPStatus != tt.status {
				t.Errorf("HTTPStatus = %d, want %d", tt.err.HTTPStatus, tt.status)
			}
		})
	}
}

func TestErrorsIsWorksAgainstTheConstant(t *testing.T) {
	wrapped := errors.Join(errs.StorageUnavailable, errors.New("dial tcp: refused"))
	if !errors.Is(wrapped, errs.StorageUnavailable) {
		t.Error("errors.Is falhou contra a constante")
	}
	if errors.Is(wrapped, errs.QueueUnavailable) {
		t.Error("errors.Is casou com a constante errada")
	}
}

func TestMissingConfigHasNoHTTPStatus(t *testing.T) {
	// Falta de configuração mata o processo antes de a porta abrir; a
	// constante existe pelo nível, não pelo status.
	if errs.MissingConfig.HTTPStatus != 0 {
		t.Errorf("HTTPStatus = %d, want 0", errs.MissingConfig.HTTPStatus)
	}
}
```

`internal/errs/catalog_test.go`:

```go
package errs_test

import (
	"testing"

	"github.com/victorzix/vhook/i18n"
	"github.com/victorzix/vhook/internal/errs"
)

// É o teste que ARCHITECTURE.md §4.29 exige: código sem mensagem passa em
// review e aparece em produção como string vazia.
func TestEveryCodeHasAMessageInEveryLocale(t *testing.T) {
	for _, locale := range i18n.Locales {
		catalogue, err := i18n.Load(locale)
		if err != nil {
			t.Fatalf("carregar %s: %v", locale, err)
		}
		for _, e := range errs.All() {
			msg, ok := catalogue[e.Code]
			if !ok {
				t.Errorf("%s: falta %s no catálogo", locale, e.Code)
				continue
			}
			if msg == "" {
				t.Errorf("%s: %s tem mensagem vazia", locale, e.Code)
			}
		}
	}
}

func TestNoOrphanEntriesInAnyLocale(t *testing.T) {
	registered := map[string]bool{}
	for _, e := range errs.All() {
		registered[e.Code] = true
	}
	for _, locale := range i18n.Locales {
		catalogue, err := i18n.Load(locale)
		if err != nil {
			t.Fatalf("carregar %s: %v", locale, err)
		}
		for code := range catalogue {
			if !registered[code] {
				t.Errorf("%s: %s tem mensagem mas não está no registro", locale, code)
			}
		}
	}
}
```

- [ ] **Step 4: Rodar os testes para ver falhar**

```bash
go test ./internal/errs/ -v
```

Esperado: FAIL na compilação, `undefined: errs.All`, `undefined: errs.StorageUnavailable`.

- [ ] **Step 5: Implementar**

`internal/errs/errs.go`:

```go
// Package errs is the error registry: code, level and HTTP status, and no
// text at all. Text lives in the i18n catalogue, indexed by code. Keeping the
// two apart is what stops them from drifting. See docs/ERRORS.md.
package errs

import "net/http"

// Level is the default logging level carried by an error. A call site may
// escalate it when the context justifies — an endpoint answering 503 is warn,
// the same endpoint tripping the circuit breaker is error.
type Level string

const (
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Type is a failure class. It supplies the default level and HTTP status so
// that neither is decided ad hoc at each call site.
type Type struct {
	Code   string
	Level  Level
	Status int
}

var (
	TypeVAL = Type{"VAL", LevelWarn, http.StatusUnprocessableEntity}
	TypeCRD = Type{"CRD", LevelWarn, http.StatusUnauthorized}
	TypePRM = Type{"PRM", LevelWarn, http.StatusForbidden}
	TypeNFD = Type{"NFD", LevelWarn, http.StatusNotFound}
	TypeCFL = Type{"CFL", LevelWarn, http.StatusConflict}
	TypeLMT = Type{"LMT", LevelWarn, http.StatusTooManyRequests}
	TypeDEP = Type{"DEP", LevelError, http.StatusBadGateway}
	TypeTMO = Type{"TMO", LevelError, http.StatusGatewayTimeout}
	TypeINT = Type{"INT", LevelError, http.StatusInternalServerError}
)

// Error is a registered failure. It is compared with errors.Is against the
// constant, never by matching message text.
type Error struct {
	Code       string
	Level      Level
	HTTPStatus int
}

func (e *Error) Error() string { return e.Code }

type option func(*Error)

// withStatus overrides the status the type would supply. Every use needs a
// reason recorded in the spec that introduced it.
func withStatus(s int) option { return func(e *Error) { e.HTTPStatus = s } }

// withLevel overrides the level the type would supply.
func withLevel(l Level) option { return func(e *Error) { e.Level = l } }

// noHTTPStatus marks an error that never becomes a response.
func noHTTPStatus() option { return func(e *Error) { e.HTTPStatus = 0 } }

var registry []*Error

func register(code string, t Type, opts ...option) *Error {
	e := &Error{Code: code, Level: t.Level, HTTPStatus: t.Status}
	for _, opt := range opts {
		opt(e)
	}
	registry = append(registry, e)
	return e
}

// All returns every registered error. The completeness test walks it.
func All() []*Error { return registry }

var (
	// StorageUnavailable overrides DEP's 502: 502 means "an upstream answered
	// badly"; this means "not ready to serve", which is 503 to orchestrators.
	StorageUnavailable = register("STO-DEP-001", TypeDEP, withStatus(http.StatusServiceUnavailable))

	// QueueUnavailable overrides DEP's 502 for the same reason.
	QueueUnavailable = register("QUE-DEP-001", TypeDEP, withStatus(http.StatusServiceUnavailable))

	// Draining overrides DEP's error level: shutting down is the normal path
	// of a deploy, and logging it as error trains operators to ignore error.
	Draining = register("SYS-DEP-001", TypeDEP,
		withStatus(http.StatusServiceUnavailable), withLevel(LevelWarn))

	Internal = register("SYS-INT-001", TypeINT)

	// MissingConfig never becomes a response: the process exits before the
	// port opens. It carries a level for the log.
	MissingConfig = register("CFG-VAL-001", TypeVAL, noHTTPStatus())
)
```

- [ ] **Step 6: Rodar os testes para ver passar**

```bash
go test ./internal/errs/ -v
```

Esperado: PASS. Se `TestEveryCodeHasAMessageInEveryLocale` falhar, falta entrada num locale — é exatamente o que ele existe para pegar.

- [ ] **Step 7: Verificar que o teste de completude realmente pega a falta**

Remova temporariamente a linha `"SYS-DEP-001"` de `i18n/errors.fr.json` e rode:

```bash
go test ./internal/errs/ -run TestEveryCodeHasAMessageInEveryLocale -v
```

Esperado: FAIL com `fr: falta SYS-DEP-001 no catálogo`. **Restaure a linha e rode de novo até PASS.** Um teste de completude que nunca foi visto falhando não prova nada.

- [ ] **Step 8: Commit**

```bash
git add i18n internal/errs
```

Mensagem:

```
feat: registrar erros com código, nível e status
```

---

## Task 4: Schema inicial e runner de migration

**Files:**
- Create: `migrations/migrations.go`
- Create: `migrations/000001_initial_schema.up.sql`
- Create: `migrations/000001_initial_schema.down.sql`
- Create: `internal/store/postgres_test.go` (helper de container)
- Create: `internal/store/migrate_test.go`
- Create: `internal/store/migrate.go`
- Create: `internal/store/pool.go`

**Interfaces:**
- Consumes: nada.
- Produces:
  - `migrations.FS embed.FS`
  - `func store.Migrate(ctx context.Context, databaseURL string) error`
  - `func store.Rollback(ctx context.Context, databaseURL string) error`
  - `func store.NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error)`
  - `func startPostgres(t *testing.T) string` no pacote de teste — devolve a URL de conexão

- [ ] **Step 1: Adicionar as dependências**

```bash
go get github.com/golang-migrate/migrate/v4@latest
go get github.com/jackc/pgx/v5@latest
go get github.com/testcontainers/testcontainers-go/modules/postgres@latest
```

- [ ] **Step 2: Escrever a migration de subida**

`migrations/000001_initial_schema.up.sql` — a DDL é a da spec, copiada sem alteração:

```sql
CREATE TABLE organizations (
    id          uuid PRIMARY KEY,
    name        text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE applications (
    id               uuid PRIMARY KEY,
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name             text NOT NULL,
    api_key_hash     text NOT NULL,
    plan             text NOT NULL DEFAULT 'free'
                     CHECK (plan IN ('free')),
    backoff_profile  text NOT NULL DEFAULT 'production'
                     CHECK (backoff_profile IN ('production', 'demo')),
    locale           text NOT NULL DEFAULT 'pt-BR'
                     CHECK (locale IN ('pt-BR', 'en', 'es', 'fr')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (api_key_hash)
);

CREATE TABLE endpoints (
    id                    uuid PRIMARY KEY,
    application_id        uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    url                   text  NOT NULL,
    secret_encrypted      bytea NOT NULL,
    status                text  NOT NULL DEFAULT 'active'
                          CHECK (status IN ('active', 'disabled')),
    consecutive_failures  integer     NOT NULL DEFAULT 0,
    disabled_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX endpoints_application_id_idx ON endpoints (application_id);

-- payload é text e não jsonb de propósito: jsonb reordena chaves e
-- re-renderiza na leitura, o que quebraria o HMAC calculado sobre os bytes
-- crus. Ver ARCHITECTURE.md §4.32. NULL significa expurgado por retenção.
CREATE TABLE events (
    id               uuid PRIMARY KEY,
    application_id   uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    event_type       text NOT NULL,
    payload          text,
    idempotency_key  text,
    received_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (application_id, idempotency_key)
);

CREATE TABLE deliveries (
    id               uuid PRIMARY KEY,
    event_id         uuid NOT NULL REFERENCES events(id)    ON DELETE CASCADE,
    endpoint_id      uuid NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    status           text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','delivering','succeeded','failed','dead')),
    attempt_count    integer NOT NULL DEFAULT 0,
    next_attempt_at  timestamptz,
    completed_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX deliveries_keyset_idx     ON deliveries (created_at, id);
CREATE INDEX deliveries_reconciler_idx ON deliveries (status, next_attempt_at);

CREATE TABLE delivery_attempts (
    id                uuid PRIMARY KEY,
    delivery_id       uuid    NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    attempt_number    integer NOT NULL,
    status_code       integer,
    response_time_ms  integer,
    response_snippet  text,
    error             text,
    attempted_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (delivery_id, attempt_number)
);
```

- [ ] **Step 3: Escrever a migration de descida**

Ordem inversa, para não bater nas foreign keys.

`migrations/000001_initial_schema.down.sql`:

```sql
DROP TABLE IF EXISTS delivery_attempts;
DROP TABLE IF EXISTS deliveries;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS endpoints;
DROP TABLE IF EXISTS applications;
DROP TABLE IF EXISTS organizations;
```

- [ ] **Step 4: Expor as migrations como pacote Go**

`migrations/migrations.go`:

```go
// Package migrations embeds the SQL files so the api applies them from its own
// binary at boot. It is a package only because go:embed cannot reach above the
// directory of the file that declares it.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

- [ ] **Step 5: Escrever o helper de container**

`internal/store/postgres_test.go`:

```go
package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startPostgres brings up a throwaway Postgres and returns its connection URL.
// Integration tests never touch the compose database: a test that depends on
// `make up` having been run is a test that fails for the wrong reason.
func startPostgres(t *testing.T) string {
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
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("subir postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("derrubar postgres: %v", err)
		}
	})

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return url
}
```

- [ ] **Step 6: Escrever os testes que falham**

`internal/store/migrate_test.go`:

```go
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
	defer conn.Close(ctx)

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
	defer conn.Close(ctx)

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
```

- [ ] **Step 7: Rodar os testes para ver falhar**

```bash
go test ./internal/store/ -v
```

Esperado: FAIL na compilação, `undefined: store.Migrate`.

- [ ] **Step 8: Implementar o runner**

`internal/store/migrate.go`:

```go
// Package store owns everything that touches Postgres: the pool, the migration
// runner and the sqlc-generated code.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver named "pgx"

	"github.com/victorzix/vhook/migrations"
)

// Migrate applies every pending migration. golang-migrate takes a Postgres
// advisory lock for the duration, so two api instances booting at once
// serialise instead of racing: one applies, the other waits and moves on.
func Migrate(ctx context.Context, databaseURL string) error {
	return run(ctx, databaseURL, func(m *migrate.Migrate) error { return m.Up() })
}

// Rollback undoes every applied migration. Development and tests only.
func Rollback(ctx context.Context, databaseURL string) error {
	return run(ctx, databaseURL, func(m *migrate.Migrate) error { return m.Down() })
}

func run(ctx context.Context, databaseURL string, direction func(*migrate.Migrate) error) error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("store: read migrations: %w", err)
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("store: open database: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return errors.Join(errs.StorageUnavailable, fmt.Errorf("store: ping: %w", err))
	}

	driver, err := migratepg.WithInstance(db, &migratepg.Config{})
	if err != nil {
		return fmt.Errorf("store: migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "pgx", driver)
	if err != nil {
		return fmt.Errorf("store: migration runner: %w", err)
	}

	if err := direction(m); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}
```

Adicione o import `"github.com/victorzix/vhook/internal/errs"` ao bloco.

- [ ] **Step 9: Implementar o pool**

`internal/store/pool.go`:

```go
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/victorzix/vhook/internal/errs"
)

// NewPool opens the connection pool used by everything except the migration
// runner, which needs database/sql.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: parse DATABASE_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.Join(errs.StorageUnavailable, fmt.Errorf("store: pool: %w", err))
	}
	return pool, nil
}
```

- [ ] **Step 10: Rodar os testes para ver passar**

```bash
go test ./internal/store/ -v
```

Esperado: PASS nos cinco testes. Levam dezenas de segundos — cada um sobe o próprio container.

- [ ] **Step 11: Verificar que `-short` pula**

```bash
go test ./internal/store/ -short -v
```

Esperado: todos como SKIP com "integração: precisa de Docker".

- [ ] **Step 12: Commit**

```bash
git add migrations internal/store go.mod go.sum
```

Mensagem:

```
feat: criar schema inicial e runner de migration
```

---

## Task 5: Função SQL `vhook_id`

**Files:**
- Create: `migrations/000002_vhook_id_function.up.sql`
- Create: `migrations/000002_vhook_id_function.down.sql`
- Create: `internal/store/vhook_id_test.go`

**Interfaces:**
- Consumes: `store.Migrate` da Task 4; os vetores de `internal/ids/testdata/vectors.json` da Task 2.
- Produces: função SQL `vhook_id(text) → uuid`.

- [ ] **Step 1: Escrever o teste que falha**

O teste lê o **mesmo arquivo de vetores** que o teste Go de `internal/ids`. Compartilhar o arquivo é o ponto inteiro: é o que impede as duas implementações do encoding de divergirem. O caminho relativo é feio de propósito — deixa visível que existe um acoplamento intencional.

`internal/store/vhook_id_test.go`:

```go
package store_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
```

Adicione ao fim do arquivo, com o import de `"github.com/jackc/pgx/v5/pgconn"`:

```go
func isSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
```

E o import de `"errors"`.

- [ ] **Step 2: Rodar o teste para ver falhar**

```bash
go test ./internal/store/ -run TestVhookID -v
```

Esperado: FAIL com `function vhook_id(unknown) does not exist`.

- [ ] **Step 3: Implementar a função**

`migrations/000002_vhook_id_function.up.sql`:

```sql
-- Decodifica a forma externa de um identificador de volta para o uuid
-- armazenado, para que uma investigação escreva direto no psql:
--
--   SELECT * FROM events WHERE id = vhook_id('evt_01HX62MYSHFH79MARZBJ6KWTR4');
--
-- Existe porque o psql mostra o uuid cru, e a API mostra base32 (§4.31).
-- Um teste de integração roda os mesmos vetores contra esta função e contra
-- a implementação Go, porque duas cópias do mesmo encoding divergem sozinhas.
CREATE OR REPLACE FUNCTION vhook_id(external text)
RETURNS uuid
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    alphabet CONSTANT text := '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
    hexdigits CONSTANT text := '0123456789abcdef';
    max128 CONSTANT numeric := 340282366920938463463374607431768211455;
    body text;
    acc  numeric := 0;
    pos  integer;
    hex  text := '';
BEGIN
    body := upper(external);

    -- Prefixo de recurso é opcional e não é validado aqui: quem valida que
    -- um evt_ não virou dlv_ é a camada de aplicação, com erro nomeado.
    IF body ~ '^[A-Z]{3}_' THEN
        body := substring(body from 5);
    END IF;

    -- Ambiguidades do alfabeto Crockford.
    body := translate(body, 'ILO', '110');

    IF body !~ '^[0-9A-Z]{26}$' THEN
        RAISE EXCEPTION 'invalid vhook id: %', external
            USING ERRCODE = 'invalid_text_representation';
    END IF;

    FOR i IN 1..26 LOOP
        pos := position(substring(body from i for 1) in alphabet);
        IF pos = 0 THEN
            RAISE EXCEPTION 'invalid vhook id: %', external
                USING ERRCODE = 'invalid_text_representation';
        END IF;
        acc := acc * 32 + (pos - 1);
    END LOOP;

    -- 26 caracteres carregam 130 bits; só 128 são válidos.
    IF acc > max128 THEN
        RAISE EXCEPTION 'invalid vhook id: %', external
            USING ERRCODE = 'invalid_text_representation';
    END IF;

    FOR i IN 1..32 LOOP
        hex := substring(hexdigits from (mod(acc, 16)::integer + 1) for 1) || hex;
        acc := div(acc, 16);
    END LOOP;

    RETURN hex::uuid;
END;
$$;
```

`migrations/000002_vhook_id_function.down.sql`:

```sql
DROP FUNCTION IF EXISTS vhook_id(text);
```

- [ ] **Step 4: Rodar o teste para ver passar**

```bash
go test ./internal/store/ -run TestVhookID -v
```

Esperado: PASS nos três testes e em todos os subtestes.

- [ ] **Step 5: Rodar a suíte inteira do pacote**

```bash
go test ./internal/store/ -v
```

Esperado: PASS. `TestRollbackEmptiesTheSchema` continua verde — a `000002` só derruba a função, e a consulta de tabelas ignora funções.

- [ ] **Step 6: Commit**

```bash
git add migrations internal/store
```

Mensagem:

```
feat: decodificar identificador externo no postgres
```

---

## Task 6: `make generate` — sqlc e oapi-codegen

**Files:**
- Create: `sqlc.yaml`
- Create: `contracts/oapi-codegen.yaml`
- Create: `internal/store/queries/health.sql`
- Create: `internal/store/sqlc/` (gerado)
- Create: `internal/openapi/openapi.gen.go` (gerado)
- Modify: `go.mod` (diretivas `tool`)
- Modify: `internal/CLAUDE.md` (registrar `internal/openapi` como gerado)

**Interfaces:**
- Consumes: o schema da Task 4; `contracts/openapi.yaml`.
- Produces:
  - `sqlc.New(db) *sqlc.Queries` com `func (q *Queries) Ping(ctx context.Context) (int32, error)`
  - Tipos `openapi.Health`, `openapi.Ready`, `openapi.ReadyChecks`, `openapi.Error`, `openapi.ErrorBody`, `openapi.ErrorDetail`, `openapi.ErrorCode`
  - `openapi.ServerInterface` com `GetHealth`, `GetReadiness`, `GetMetrics`, e `openapi.HandlerFromMux`

- [ ] **Step 1: Fixar as ferramentas no `go.mod`**

A diretiva `tool` do Go 1.24 mantém a versão do gerador no `go.mod`. Isso importa porque o CI roda `make generate` e compara com `git diff`: se a versão do gerador variar entre máquinas, o PR fica vermelho por motivo errado.

```bash
go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
```

- [ ] **Step 2: Escrever a query de health**

Não é query de fachada: `pool.Ping` só verifica que a conexão está aberta, enquanto uma consulta de verdade prova que o banco responde. É o que a readiness da Task 8 usa.

`internal/store/queries/health.sql`:

```sql
-- name: Ping :one
SELECT 1 AS ok;
```

- [ ] **Step 3: Configurar o sqlc**

`schema` aponta para o arquivo da tabela, não para o diretório: apontar para `migrations/` faria o sqlc ler os `.down.sql` e concluir que as tabelas foram derrubadas.

`sqlc.yaml`:

```yaml
version: "2"
sql:
  - engine: postgresql
    # Só o arquivo de tabelas: os .down.sql derrubariam o modelo, e a
    # 000002 é plpgsql, que o parser do sqlc não precisa entender.
    schema: migrations/000001_initial_schema.up.sql
    queries: internal/store/queries
    gen:
      go:
        package: sqlc
        out: internal/store/sqlc
        sql_package: pgx/v5
        emit_json_tags: false
        emit_empty_slices: true
```

- [ ] **Step 4: Configurar o oapi-codegen**

`contracts/oapi-codegen.yaml`:

```yaml
package: openapi
output: internal/openapi/openapi.gen.go
output-options:
  skip-prune: true
generate:
  models: true
  chi-server: true
  embedded-spec: false
```

`skip-prune` mantém no gerado os schemas que ainda não são referenciados por nenhuma rota — sem ele, `ErrorDetail` e `ErrorBody` sumiriam até existir uma rota que os cite, e a Task 8 precisa deles.

- [ ] **Step 5: Gerar**

```bash
make generate
```

Esperado: `internal/store/sqlc/` com `db.go`, `models.go` e `health.sql.go`; `internal/openapi/openapi.gen.go` criado.

- [ ] **Step 6: Escrever o teste que prova que o gerado bate com o schema**

`internal/store/sqlc/models_test.go`:

```go
package sqlc_test

import (
	"reflect"
	"testing"

	"github.com/victorzix/vhook/internal/store/sqlc"
)

// O sqlc gera os modelos a partir da migration. Este teste falha se alguém
// editar o gerado à mão ou se a migration mudar sem regenerar — que é o furo
// que "contrato como fonte única" existe para fechar.
func TestEventPayloadIsAStringAndNullable(t *testing.T) {
	field, ok := reflect.TypeOf(sqlc.Event{}).FieldByName("Payload")
	if !ok {
		t.Fatal("Event não tem campo Payload")
	}
	// pgtype.Text e não []byte nem um tipo de json: payload é text
	// byte-exato (§4.32), e é nullable porque NULL = expurgado.
	if got := field.Type.String(); got != "pgtype.Text" {
		t.Errorf("Payload é %s, queria pgtype.Text", got)
	}
}

func TestEveryTableGotAModel(t *testing.T) {
	models := []any{
		sqlc.Organization{}, sqlc.Application{}, sqlc.Endpoint{},
		sqlc.Event{}, sqlc.Delivery{}, sqlc.DeliveryAttempt{},
	}
	for _, m := range models {
		if reflect.TypeOf(m).NumField() == 0 {
			t.Errorf("%T não tem campo nenhum", m)
		}
	}
}
```

- [ ] **Step 7: Rodar o teste**

```bash
go test ./internal/store/sqlc/ -v
```

Esperado: PASS. Se `TestEventPayloadIsAStringAndNullable` falhar dizendo outro tipo, o sqlc mapeou `text` nullable de forma diferente da esperada — **ajuste a asserção para o tipo real e não a migration**, porque o tipo da coluna está certo e é o teste que precisa refletir a ferramenta.

- [ ] **Step 8: Registrar o pacote gerado na documentação**

Em `internal/CLAUDE.md`, dentro do bloco de layout, acrescente a linha do pacote gerado logo acima de `ids/`:

```
├── openapi/     tipos GERADOS de contracts/openapi.yaml — nunca editar
```

E em `docs/ARCHITECTURE.md` §4.24, a mesma linha no bloco de layout.

- [ ] **Step 9: Verificar que o CI ficaria verde**

```bash
make generate
git diff --exit-code
```

Esperado: sem saída e código de saída 0. Se houver diff, o gerado commitado está atrasado — commite o resultado.

- [ ] **Step 10: Commit**

```bash
git add sqlc.yaml contracts/oapi-codegen.yaml internal/store internal/openapi internal/CLAUDE.md docs/ARCHITECTURE.md go.mod go.sum
```

Mensagem:

```
chore: gerar tipos do schema e do contrato
```

---

## Task 7: `internal/obs` — log, correlation e recover

**Files:**
- Create: `internal/obs/log_test.go`
- Create: `internal/obs/log.go`
- Create: `internal/obs/httperr.go`
- Create: `internal/obs/middleware_test.go`
- Create: `internal/obs/middleware.go`

**Interfaces:**
- Consumes: `ids.New`, `ids.Render`, `errs.Error`, `openapi.Error`.
- Produces:
  - `func NewLogger(w io.Writer, level slog.Level) *slog.Logger`
  - `func CorrelationID(ctx context.Context) string`
  - `func Correlation(next http.Handler) http.Handler`
  - `func RequestLog(logger *slog.Logger) func(http.Handler) http.Handler`
  - `func Recover(logger *slog.Logger) func(http.Handler) http.Handler`
  - `func ValidClientCorrelationID(v string) bool`
  - `func WriteError(w http.ResponseWriter, r *http.Request, e *errs.Error, details ...openapi.ErrorDetail)`
  - Constantes `HeaderCorrelationID = "X-Vhook-Correlation-Id"` e `HeaderClientCorrelationID = "X-Correlation-Id"`

- [ ] **Step 1: Adicionar a dependência de chi**

```bash
go get github.com/go-chi/chi/v5@latest
```

- [ ] **Step 2: Escrever os testes que falham**

`internal/obs/middleware_test.go`:

```go
package obs_test

import (
	"encoding/json"
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
```

Acrescente `"io"` e `"log/slog"` aos imports.

`internal/obs/log_test.go`:

```go
package obs_test

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/victorzix/vhook/internal/obs"
)

func TestLoggerWritesJSON(t *testing.T) {
	var out strings.Builder
	obs.NewLogger(&out, slog.LevelInfo).Info("subiu", "port", 8080)

	var line map[string]any
	if err := json.Unmarshal([]byte(out.String()), &line); err != nil {
		t.Fatalf("log não é JSON: %v — %q", err, out.String())
	}
	if line["msg"] != "subiu" {
		t.Errorf("msg = %v, want subiu", line["msg"])
	}
}

func TestClientCorrelationIDIsLoggedSeparately(t *testing.T) {
	var out strings.Builder
	logger := obs.NewLogger(&out, slog.LevelInfo)

	obs.LogRequest(logger, "cliente-123", true).Info("requisição")

	var line map[string]any
	if err := json.Unmarshal([]byte(out.String()), &line); err != nil {
		t.Fatalf("log não é JSON: %v", err)
	}
	if line["client_correlation_id"] != "cliente-123" {
		t.Errorf("client_correlation_id = %v", line["client_correlation_id"])
	}
}

func TestInvalidClientCorrelationIDIsMarkedAsDropped(t *testing.T) {
	var out strings.Builder
	logger := obs.NewLogger(&out, slog.LevelInfo)

	obs.LogRequest(logger, strings.Repeat("x", 65), false).Info("requisição")

	var line map[string]any
	if err := json.Unmarshal([]byte(out.String()), &line); err != nil {
		t.Fatalf("log não é JSON: %v", err)
	}
	if line["client_correlation_id_dropped"] != true {
		t.Error("valor inválido devia ter sido marcado como descartado")
	}
	if _, present := line["client_correlation_id"]; present {
		t.Error("valor inválido não pode aparecer no log")
	}
}
```

- [ ] **Step 3: Rodar os testes para ver falhar**

```bash
go test ./internal/obs/ -v
```

Esperado: FAIL na compilação, `undefined: obs.NewLogger`.

- [ ] **Step 4: Implementar o logger**

`internal/obs/log.go`:

```go
// Package obs holds the cross-cutting observability surface: structured
// logging, the correlation id that follows a request across processes, the
// Prometheus endpoint and the health handlers.
package obs

import (
	"context"
	"io"
	"log/slog"
)

// Header names. The first is ours and always present; the second is what a
// producer may send so its own logs can be joined to ours.
const (
	HeaderCorrelationID       = "X-Vhook-Correlation-Id"
	HeaderClientCorrelationID = "X-Correlation-Id"
)

type ctxKey struct{}

// NewLogger returns the structured logger used by every process.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}

// WithCorrelationID puts the trace id on the context so services and repos
// can log it without taking an http.Request.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// CorrelationID returns the trace id, or "" outside a request.
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// LogRequest decorates a logger with what the client sent. A valid client
// value is recorded in its own field and never reused as our trace id; an
// invalid one is recorded only as having been dropped, because it is
// attacker-controlled text and does not belong in a searchable field.
func LogRequest(logger *slog.Logger, clientID string, valid bool) *slog.Logger {
	switch {
	case clientID == "":
		return logger
	case valid:
		return logger.With("client_correlation_id", clientID)
	default:
		return logger.With("client_correlation_id_dropped", true)
	}
}
```

- [ ] **Step 5: Implementar a serialização de erro**

`internal/obs/httperr.go`:

```go
package obs

import (
	"encoding/json"
	"net/http"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/openapi"
)

// WriteError renders the project's error envelope: code, correlation id and
// optional per-field details — never a message. The dashboard translates the
// code through the i18n catalogue. See ARCHITECTURE.md §4.29.
func WriteError(w http.ResponseWriter, r *http.Request, e *errs.Error, details ...openapi.ErrorDetail) {
	body := openapi.Error{
		Error: openapi.ErrorBody{
			Code:          openapi.ErrorCode(e.Code),
			CorrelationId: CorrelationID(r.Context()),
		},
	}
	if len(details) > 0 {
		body.Error.Details = &details
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.HTTPStatus)
	_ = json.NewEncoder(w).Encode(body)
}
```

Se o gerador tiver nomeado o campo de forma diferente de `CorrelationId` ou não tiver usado ponteiro em `Details`, **ajuste este arquivo ao gerado, nunca o contrário** — o gerado é a fonte.

- [ ] **Step 6: Implementar os middlewares**

`internal/obs/middleware.go`:

```go
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
```

- [ ] **Step 7: Rodar os testes para ver passar**

```bash
go test ./internal/obs/ -v
```

Esperado: PASS em todos.

- [ ] **Step 8: Commit**

```bash
git add internal/obs go.mod go.sum
```

Mensagem:

```
feat: propagar correlation id e capturar panic
```

---

## Task 8: `internal/obs` — healthz, readyz e metrics

**Files:**
- Create: `internal/obs/health_test.go`
- Create: `internal/obs/health.go`

**Interfaces:**
- Consumes: `WriteError`, `errs.StorageUnavailable`, `errs.QueueUnavailable`, `errs.Draining`, `openapi.*`.
- Produces:
  - `type Check struct { Name string; Err *errs.Error; Ping func(context.Context) error }`
  - `func NewHealth(logger *slog.Logger, checks ...Check) *Health`
  - `func (h *Health) SetCheckTimeout(d time.Duration)`
  - `func (h *Health) Drain()`
  - `func (h *Health) GetHealth(w http.ResponseWriter, r *http.Request)`
  - `func (h *Health) GetReadiness(w http.ResponseWriter, r *http.Request)`
  - `func (h *Health) GetMetrics(w http.ResponseWriter, r *http.Request)`
  - `func RegisterBuildInfo(version, commit string)`
  - `*Health` satisfaz `openapi.ServerInterface`

- [ ] **Step 1: Adicionar a dependência do Prometheus**

```bash
go get github.com/prometheus/client_golang@latest
```

- [ ] **Step 2: Escrever os testes que falham**

`internal/obs/health_test.go`:

```go
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
```

- [ ] **Step 3: Rodar os testes para ver falhar**

```bash
go test ./internal/obs/ -run 'TestHealthz|TestReadyz|TestMetrics|TestHealth' -v
```

Esperado: FAIL na compilação, `undefined: obs.NewHealth`.

- [ ] **Step 4: Implementar**

`internal/obs/health.go`:

```go
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
	writeJSON(w, http.StatusOK, openapi.Health{Status: "ok"})
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
		Status: "ready",
		Checks: openapi.ReadyChecks{Postgres: "ok", Rabbitmq: "ok"},
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
```

- [ ] **Step 5: Rodar os testes para ver passar**

```bash
go test ./internal/obs/ -v
```

Esperado: PASS em todos, incluindo `TestHealthSatisfiesTheGeneratedInterface` — é ele que prova que o handler continua casando com o contrato.

- [ ] **Step 6: Commit**

```bash
git add internal/obs go.mod go.sum
```

Mensagem:

```
feat: expor healthz, readyz e metrics
```

---

## Task 9: `cmd/api`, Dockerfile e a release

**Files:**
- Create: `cmd/api/config_test.go`
- Create: `cmd/api/config.go`
- Create: `cmd/api/server.go`
- Create: `cmd/api/server_test.go`
- Create: `cmd/api/main.go`
- Create: `Dockerfile`
- Modify: `docker-compose.yml` (serviço `api`)
- Modify: `CLAUDE.md` (seção `## Comandos`)

**Interfaces:**
- Consumes: tudo das tasks anteriores.
- Produces:
  - `func loadConfig() (config, error)`
  - `func newRouter(logger *slog.Logger, health *obs.Health) http.Handler`
  - `func postgresCheck(pool *pgxpool.Pool) obs.Check`
  - `func rabbitCheck(url string) obs.Check`
  - Binário `api` e imagem Docker.

- [ ] **Step 1: Adicionar a dependência de AMQP**

```bash
go get github.com/rabbitmq/amqp091-go@latest
```

- [ ] **Step 2: Escrever o teste de configuração**

`cmd/api/config_test.go`:

```go
package main

import (
	"errors"
	"testing"

	"github.com/victorzix/vhook/internal/errs"
)

func TestLoadConfigReadsTheThreeVariables(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/vhook")
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("VHOOK_HTTP_ADDR", ":9090")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.httpAddr != ":9090" {
		t.Errorf("httpAddr = %q, want :9090", cfg.httpAddr)
	}
}

func TestLoadConfigDefaultsTheHTTPAddress(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/vhook")
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("VHOOK_HTTP_ADDR", "")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.httpAddr != ":8080" {
		t.Errorf("httpAddr = %q, want :8080", cfg.httpAddr)
	}
}

func TestLoadConfigFailsWhenASecretIsMissing(t *testing.T) {
	tests := []struct{ name, unset string }{
		{"sem banco", "DATABASE_URL"},
		{"sem fila", "RABBITMQ_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/vhook")
			t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
			t.Setenv(tt.unset, "")

			_, err := loadConfig()
			if !errors.Is(err, errs.MissingConfig) {
				t.Errorf("error = %v, queria errs.MissingConfig", err)
			}
			// A mensagem precisa nomear a variável: sem isso o operador
			// descobre qual falta por tentativa e erro.
			if err == nil || !contains(err.Error(), tt.unset) {
				t.Errorf("error = %v, devia nomear %s", err, tt.unset)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && strings.Contains(haystack, needle)
}
```

Acrescente `"strings"` aos imports.

- [ ] **Step 3: Rodar para ver falhar**

```bash
go test ./cmd/api/ -v
```

Esperado: FAIL, `undefined: loadConfig`.

- [ ] **Step 4: Implementar a configuração**

`cmd/api/config.go`:

```go
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/victorzix/vhook/internal/errs"
)

// config holds the whole environment surface of the api: two infrastructure
// addresses and the bind address. Everything else — timeouts, shard count,
// backoff profiles — is code or a database column, never an environment
// variable. See ARCHITECTURE.md §4.25.
type config struct {
	databaseURL string
	rabbitURL   string
	httpAddr    string
}

const defaultHTTPAddr = ":8080"

func loadConfig() (config, error) {
	cfg := config{
		databaseURL: os.Getenv("DATABASE_URL"),
		rabbitURL:   os.Getenv("RABBITMQ_URL"),
		httpAddr:    os.Getenv("VHOOK_HTTP_ADDR"),
	}

	var missing []string
	if cfg.databaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.rabbitURL == "" {
		missing = append(missing, "RABBITMQ_URL")
	}
	if len(missing) > 0 {
		return config{}, errors.Join(errs.MissingConfig,
			fmt.Errorf("config: missing %v", missing))
	}

	if cfg.httpAddr == "" {
		cfg.httpAddr = defaultHTTPAddr
	}
	return cfg, nil
}
```

- [ ] **Step 5: Rodar para ver passar**

```bash
go test ./cmd/api/ -v
```

Esperado: PASS nos quatro.

- [ ] **Step 6: Escrever o teste de integração ponta a ponta**

`cmd/api/server_test.go`:

```go
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

	"github.com/jackc/pgx/v5/pgxpool"
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
```

Instale o módulo de RabbitMQ do testcontainers:

```bash
go get github.com/testcontainers/testcontainers-go/modules/rabbitmq@latest
```

- [ ] **Step 7: Rodar para ver falhar**

```bash
go test ./cmd/api/ -run TestServerAnswers -v
```

Esperado: FAIL na compilação, `undefined: newRouter`, `undefined: postgresCheck`.

- [ ] **Step 8: Implementar o router e as checagens**

`cmd/api/server.go`:

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/obs"
	"github.com/victorzix/vhook/internal/openapi"
	"github.com/victorzix/vhook/internal/store/sqlc"
)

// newRouter wires the middleware chain and the generated route table.
// Route groups with distinct authentication middleware arrive with the first
// authenticated surface; these three routes are public by nature.
func newRouter(logger *slog.Logger, health *obs.Health) http.Handler {
	r := chi.NewRouter()
	// A ordem importa: Correlation primeiro, para que o id já exista quando
	// RequestLog e Recover forem escrever.
	r.Use(obs.Correlation)
	r.Use(obs.RequestLog(logger))
	r.Use(obs.Recover(logger))
	return openapi.HandlerFromMux(health, r)
}

// postgresCheck runs a real query rather than pool.Ping: Ping only proves the
// connection is open, a query proves the database answers.
func postgresCheck(pool *pgxpool.Pool) obs.Check {
	return obs.Check{
		Name: "postgres",
		Err:  errs.StorageUnavailable,
		Ping: func(ctx context.Context) error {
			_, err := sqlc.New(pool).Ping(ctx)
			return err
		},
	}
}

// rabbitCheck dials a fresh connection each probe. This release publishes
// nothing, so there is no long-lived connection to keep alive; the persistent
// connection and its reconnect logic arrive with the queue spec.
func rabbitCheck(url string) obs.Check {
	return obs.Check{
		Name: "rabbitmq",
		Err:  errs.QueueUnavailable,
		Ping: func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			timeout := 2 * time.Second
			if ok {
				timeout = time.Until(deadline)
			}
			conn, err := amqp.DialConfig(url, amqp.Config{
				Dial: amqp.DefaultDial(timeout),
			})
			if err != nil {
				return err
			}
			return conn.Close()
		},
	}
}
```

- [ ] **Step 9: Rodar para ver passar**

```bash
go test ./cmd/api/ -run TestServerAnswers -v
```

Esperado: PASS nos cinco subtestes, **na ordem em que estão escritos**. Este teste não pode rodar com `-shuffle` dentro do próprio `t.Run`: os dois últimos derrubam containers e dependem de vir depois dos três primeiros. O `-shuffle=on` do CI embaralha funções de teste, não subtestes, então isso é seguro.

- [ ] **Step 10: Escrever o `main.go`**

Não tem teste próprio: tudo que ele faz de testável já está em `loadConfig`, `newRouter` e nas checagens. O que sobra é sequência de boot e sinal, exercitado pela verificação manual do Step 13.

`cmd/api/main.go`:

```go
// Command api serves the ingress and management surfaces of vhook. It applies
// pending migrations at boot under a Postgres advisory lock, then serves HTTP.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/victorzix/vhook/internal/obs"
	"github.com/victorzix/vhook/internal/store"
)

// Injected at build time with -ldflags.
var (
	version = "dev"
	commit  = "none"
)

// drainGrace is how long readiness reports 503 before the server stops
// accepting connections, so a load balancer notices before requests are cut.
const drainGrace = 5 * time.Second

func main() {
	logger := obs.NewLogger(os.Stdout, slog.LevelInfo)
	if err := run(logger); err != nil {
		logger.Error("shutting down after failure", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Migrations before anything else, and before the port opens: a process
	// that serves against an outdated schema fails in ways that look like
	// application bugs.
	if err := store.Migrate(ctx, cfg.databaseURL); err != nil {
		return err
	}

	pool, err := store.NewPool(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	obs.RegisterBuildInfo(version, commit)

	// Check order decides which code leads a 503, so it is fixed here.
	health := obs.NewHealth(logger, postgresCheck(pool), rabbitCheck(cfg.rabbitURL))

	server := &http.Server{
		Addr:              cfg.httpAddr,
		Handler:           newRouter(logger, health),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.httpAddr, "version", version)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("draining", "grace", drainGrace.String())
	health.Drain()
	time.Sleep(drainGrace)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}
```

- [ ] **Step 11: Escrever o `Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/api ./cmd/api

# Estático e sem shell: a imagem não tem nada para um atacante usar, e o
# binário Go não precisa de libc.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api"]
```

- [ ] **Step 12: Acrescentar o serviço `api` ao compose**

Agora o arquivo descreve o deploy inteiro. Localmente você continua subindo só a infraestrutura, porque `make up` nomeia os serviços — sem `profiles`, que seria mais uma coisa para o Coolify precisar ativar.

```yaml
  api:
    build:
      context: .
      args:
        VERSION: dev
        COMMIT: local
    environment:
      DATABASE_URL: postgres://vhook:vhook@postgres:5432/vhook?sslmode=disable
      RABBITMQ_URL: amqp://guest:guest@rabbitmq:5672/
      VHOOK_HTTP_ADDR: ":8080"
    ports:
      - "8080:8080"
    depends_on:
      postgres:
        condition: service_healthy
      rabbitmq:
        condition: service_healthy
```

- [ ] **Step 13: Verificação manual**

```bash
make up
cp .env.example .env
set -a && . ./.env && set +a
make run
```

Noutro terminal:

```bash
curl -i  localhost:8080/healthz
curl -s  localhost:8080/readyz | jq
curl -s  localhost:8080/metrics | grep vhook_build_info
curl -i -H 'X-Correlation-Id: cliente-123' localhost:8080/healthz

docker compose stop postgres
curl -i localhost:8080/readyz    # 503 STO-DEP-001
curl -i localhost:8080/healthz   # ainda 200
docker compose start postgres
curl -s localhost:8080/readyz | jq
```

Esperado, na ordem: 200 com `X-Vhook-Correlation-Id`; `{"status":"ready","checks":{...}}`; a linha de `vhook_build_info`; no terminal da `api`, uma linha JSON com `client_correlation_id: "cliente-123"`. Depois o par 503/200 e a volta ao `ready`.

Encerre com `Ctrl+C` e confirme no log a linha `draining` antes de o processo sair.

- [ ] **Step 13b: Verificar os dois modos de falha de boot**

São os únicos da spec sem cobertura automatizada, porque testá-los exigiria controlar o ciclo de vida do processo a partir de um teste.

```bash
docker compose stop postgres
make run          # esperado: sai com código != 0 e log de erro
echo $?           # esperado: 1

docker compose start postgres
docker compose stop rabbitmq
make run          # esperado: SOBE, porque nada publica ainda
```

Com o Rabbit parado e a `api` de pé, noutro terminal:

```bash
curl -i localhost:8080/readyz    # 503 QUE-DEP-001
curl -i localhost:8080/healthz   # 200
```

Depois `docker compose start rabbitmq` e confirme que `/readyz` volta a 200 sem reiniciar a `api` — é o que prova que a checagem disca a cada probe em vez de guardar conexão.

**O modo de falha "migration falha no meio" não é testado.** Marcar a versão como `dirty` e recusar todo boot seguinte é comportamento documentado do `golang-migrate`, e forçar esse estado num teste exigiria uma migration deliberadamente quebrada no diretório — que passaria a rodar em todo teste de integração do projeto. Fica registrado como comportamento herdado, não verificado.

- [ ] **Step 14: Verificar que a imagem builda**

```bash
docker compose build api
```

Esperado: build completo. É o que garante que o compose está pronto para o Coolify apontar.

- [ ] **Step 15: Preencher a seção de comandos**

Em `CLAUDE.md`, substitua a seção `## Comandos`:

```markdown
## Comandos

| Comando | O que faz |
|---|---|
| `make up` | sobe Postgres e RabbitMQ; a `api` roda local |
| `make down` | derruba a infraestrutura |
| `make run` | sobe a `api`, aplicando migrations no boot |
| `make generate` | regenera sqlc e oapi-codegen; o CI falha se o commitado estiver atrasado |
| `make test` | só unidade, com `-short` |
| `make test-integration` | tudo, subindo container de verdade — é o que o CI roda |

Copie `.env.example` para `.env` antes do primeiro `make run`.
```

- [ ] **Step 16: Rodar a suíte inteira**

```bash
make generate && git diff --exit-code
go vet ./...
gofmt -l .
make test-integration
```

Esperado: sem diff, sem saída do `vet` e do `gofmt`, e PASS em todos os pacotes.

- [ ] **Step 17: Commit**

```bash
git add cmd Dockerfile docker-compose.yml CLAUDE.md go.mod go.sum
```

Mensagem:

```
feat: subir a api com migrations no boot
```

Este é o `feat:` que corta a `v0.1.0` quando o PR de release for mergeado.

---

## Encerramento da spec

- [ ] **Escrever o `result.md`**

Use `docs/specs/_template_/result.md`. Registre **divergência e evidência**, nunca o que o CHANGELOG já diz. Sem divergência, uma linha dizendo isso basta.

Candidatos prováveis de divergência, com base no que este plano assume sem poder verificar:

- o tipo que o `sqlc` gerou para `events.payload` (Task 6, Step 7);
- os nomes exatos dos campos gerados pelo `oapi-codegen` — `CorrelationId`, `Details` como ponteiro (Task 7, Step 5);
- o comportamento do `tcrabbit.Run` e o nome do método que devolve a URL AMQP (Task 9, Step 6).

- [ ] **Atualizar o índice**

Em `docs/specs/README.md`, mudar o status da linha 001 de `aprovada` para `implementada` e preencher a release.

- [ ] **Republicar `docs/overview.html`**

`bash docs/pages/build.sh`, depois republicar como Artifact **passando a URL existente** — sem ela, uma conversa nova cria endereço diferente.
