# Bootstrap de tenancy — Plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dar ao vhook uma forma de criar a primeira organização e a primeira application, com uma api key utilizável — sem a qual as specs de endpoints e de ingress não têm como começar.

**Architecture:** Um binário novo, `cmd/adminctl`, com dois subcomandos. `genkey` imprime uma chave mestra; `bootstrap` gera a api key, calcula o HMAC com a chave mestra e grava organização e application numa transação. A geração e o hash vivem em `internal/apikey`, fora do comando, porque a spec de ingress vai chamar o mesmo `Hash` para autenticar.

**Tech Stack:** Go 1.26 · `jackc/pgx/v5` · `sqlc` · `crypto/hmac` + `crypto/sha256` + `crypto/rand` · `testcontainers-go`

**Spec:** [`spec.md`](spec.md) · **Release alvo:** `v0.2.0`

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
- **A chave em claro vai para stdout e mais nada.** Nunca `slog`, nunca campo estruturado, nunca disco. **A chave mestra nunca aparece em saída alguma**, nem em mensagem de erro — o erro nomeia a variável, jamais o valor.
- Teste de integração é marcado com `if testing.Short() { t.Skip(...) }`. `make test` usa `-short`.
- **`-race` não funciona nesta máquina** (`CGO_ENABLED=0`, sem compilador C). Rode sem. O CI roda `-race` em Ubuntu.
- **`make` não está instalado.** Rode o comando de dentro do alvo.
- **Para ver o red num pacote novo, crie antes o arquivo de implementação contendo só `package X`.** Sem nenhum `.go` não-teste o Go responde `no non-test Go files` e nunca chega a reclamar dos símbolos que faltam.
- **`go mod tidy` depois de todo `go get`, antes de compilar.**
- **Quem commita é o dono do repositório.** Entregue a mensagem pronta; não rode `git commit`.

---

## Estrutura de arquivos

| Arquivo | Responsabilidade |
|---|---|
| `internal/apikey/apikey.go` | `Hasher`, `NewHasher`, `Generate`, `Hash` — puro, sem I/O |
| `internal/errs/errs.go` | mais duas constantes |
| `i18n/errors.{pt-BR,en,es,fr}.json` | mais duas entradas em cada |
| `internal/store/queries/applications.sql` | entrada do `sqlc` |
| `internal/store/sqlc/applications.sql.go` | **gerado** |
| `cmd/adminctl/main.go` | despacho de subcomando e mapeamento de erro para exit code |
| `cmd/adminctl/genkey.go` | subcomando `genkey` |
| `cmd/adminctl/bootstrap.go` | subcomando `bootstrap`: flags, transação, saída |
| `cmd/adminctl/config.go` | leitura de `DATABASE_URL` e `VHOOK_MASTER_KEY` |
| `.env.example` | `VHOOK_MASTER_KEY` |

**Nenhuma migration.** O schema da 001 já tem tudo.

---

## Task 1: `internal/apikey`

O pacote que a spec de ingress vai reusar. É o de maior risco do plano: um erro aqui não dá exceção, dá 401 em chave válida.

**Files:**
- Create: `internal/apikey/apikey.go`
- Create: `internal/apikey/apikey_test.go`

**Interfaces:**
- Consumes: `errs.MissingConfig` (já existe, da spec 001).
- Produces:
  - `const Prefix = "vhk_"`
  - `type Hasher struct{ … }`
  - `func NewHasher(masterKey []byte) (*Hasher, error)`
  - `func (h *Hasher) Generate() (plain, hash string, err error)`
  - `func (h *Hasher) Hash(plain string) string`

- [ ] **Step 1: Escrever os testes que falham**

Crie antes `internal/apikey/apikey.go` com **só** a linha `package apikey`, para o red ser sobre símbolos ausentes.

`internal/apikey/apikey_test.go`:

```go
package apikey_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/victorzix/vhook/internal/apikey"
	"github.com/victorzix/vhook/internal/errs"
)

// Chaves mestras fixas: o teste do pepper precisa de duas que difiram.
var (
	masterA = []byte("0123456789abcdef0123456789abcdef")
	masterB = []byte("fedcba9876543210fedcba9876543210")
)

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func newHasher(t *testing.T, master []byte) *apikey.Hasher {
	t.Helper()
	h, err := apikey.NewHasher(master)
	if err != nil {
		t.Fatalf("NewHasher() error = %v", err)
	}
	return h
}

func TestGenerateProducesTheDocumentedFormat(t *testing.T) {
	plain, _, err := newHasher(t, masterA).Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !strings.HasPrefix(plain, apikey.Prefix) {
		t.Errorf("chave = %q, queria prefixo %q", plain, apikey.Prefix)
	}

	body := strings.TrimPrefix(plain, apikey.Prefix)
	// 43 porque 43 × log2(62) = 256,0 bits. Ver ARCHITECTURE.md §4.33.
	if len(body) != 43 {
		t.Errorf("corpo tem %d caracteres, queria 43", len(body))
	}
	for i, c := range body {
		if !strings.ContainsRune(base62, c) {
			t.Errorf("caractere %d é %q, fora do alfabeto base62", i, c)
		}
	}
}

func TestHashIsDeterministic(t *testing.T) {
	h := newHasher(t, masterA)
	const key = "vhk_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H"

	// Duas invocações em variáveis separadas, e não `h.Hash(k) != h.Hash(k)`
	// direto no if: o staticcheck recusa isso como SA4000, expressões
	// idênticas nos dois lados do operador. A asserção é a mesma — um Hash com
	// salt aleatório por chamada divergiria aqui.
	first, second := h.Hash(key), h.Hash(key)
	if first != second {
		t.Error("duas chamadas com a mesma entrada devolveram hashes diferentes")
	}
	// Instância nova, mesma chave mestra: o determinismo tem de sobreviver ao
	// processo, senão a busca por índice do ingress nunca encontraria a linha.
	if h.Hash(key) != newHasher(t, masterA).Hash(key) {
		t.Error("instâncias diferentes com a mesma chave mestra divergiram")
	}
}

func TestDifferentKeysProduceDifferentHashes(t *testing.T) {
	h := newHasher(t, masterA)
	if h.Hash("vhk_aaa") == h.Hash("vhk_bbb") {
		t.Error("chaves diferentes colidiram")
	}
}

// O teste central desta task. Sem ele, uma implementação que ignorasse a chave
// mestra e fizesse SHA-256 puro passaria em todos os outros — e o ganho inteiro
// de §4.33 estaria perdido em silêncio, sem nenhum sintoma.
func TestThePepperIsActuallyInTheComputation(t *testing.T) {
	const key = "vhk_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H"

	if newHasher(t, masterA).Hash(key) == newHasher(t, masterB).Hash(key) {
		t.Fatal("a mesma chave com chaves mestras diferentes produziu o mesmo hash: " +
			"o pepper não está entrando no cálculo")
	}
}

func TestNewHasherRejectsBadMasterKeys(t *testing.T) {
	tests := []struct {
		name   string
		master []byte
	}{
		{"nil", nil},
		{"vazia", []byte{}},
		{"curta", []byte("curta demais")},
		{"longa", []byte(strings.Repeat("x", 64))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := apikey.NewHasher(tt.master)
			if !errors.Is(err, errs.MissingConfig) {
				t.Errorf("error = %v, queria errs.MissingConfig", err)
			}
		})
	}
}

// Pega a fonte de entropia errada: um math/rand semeado com constante passaria
// em todos os testes de formato.
func TestGeneratedKeysDoNotRepeat(t *testing.T) {
	h := newHasher(t, masterA)
	seen := make(map[string]bool, 10000)
	for i := 0; i < 10000; i++ {
		plain, _, err := h.Generate()
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		if seen[plain] {
			t.Fatalf("chave repetida na iteração %d", i)
		}
		seen[plain] = true
	}
}

// Pega o viés de módulo: uma implementação com `b % 62` sobre bytes crus
// favoreceria os primeiros caracteres do alfabeto, e nenhum outro teste notaria.
func TestGeneratedKeysUseTheWholeAlphabet(t *testing.T) {
	h := newHasher(t, masterA)
	seen := map[rune]bool{}
	for i := 0; i < 10000; i++ {
		plain, _, err := h.Generate()
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		for _, c := range strings.TrimPrefix(plain, apikey.Prefix) {
			seen[c] = true
		}
	}
	for _, c := range base62 {
		if !seen[c] {
			t.Errorf("o caractere %q nunca foi sorteado em 430.000 posições", c)
		}
	}
}

func TestGenerateReturnsTheHashOfTheKeyItReturns(t *testing.T) {
	h := newHasher(t, masterA)
	plain, hash, err := h.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	// Se Generate devolvesse o hash de outra coisa, o bootstrap gravaria um
	// hash que a chave impressa nunca satisfaz — e só a spec de ingress
	// descobriria, como um 401 inexplicável.
	if hash != h.Hash(plain) {
		t.Error("o hash devolvido não é o hash da chave devolvida")
	}
}

func TestHashOfEmptyStringIsWellDefined(t *testing.T) {
	if got := newHasher(t, masterA).Hash(""); got == "" {
		t.Error("Hash(\"\") devolveu string vazia")
	}
}
```

- [ ] **Step 2: Rodar os testes para ver falhar**

```bash
go test ./internal/apikey/ -v
```

Esperado: FAIL na compilação, `undefined: apikey.NewHasher`, `undefined: apikey.Prefix`.

- [ ] **Step 3: Implementar**

`internal/apikey/apikey.go`:

```go
// Package apikey generates and verifies application API keys.
//
// The stored value is HMAC-SHA256 of the key under a server-side pepper, not a
// plain digest and never a salted slow hash: the ingress must find the
// application by indexed lookup on every request, which needs determinism, and
// a 256-bit random key has no search space that a slow hash would protect.
// See ARCHITECTURE.md §4.33.
package apikey

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/victorzix/vhook/internal/errs"
)

// Prefix makes a key recognisable in a log, a support ticket or a .env, and is
// what a secret-scanning rule would match on.
const Prefix = "vhk_"

const (
	// alphabet is Base62: no + or /, which break in URLs and in badly quoted
	// environment variables.
	alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	// keyLength is 43 because 43 × log2(62) = 256.0 bits. The number comes from
	// the entropy budget, not from taste.
	keyLength = 43

	// maxUnbiased is the largest multiple of 62 that fits in a byte (62 × 4).
	// Bytes at or above it are discarded rather than folded with %, which would
	// make the first characters of the alphabet more likely.
	maxUnbiased = 248

	// masterKeyLength matches the AES-256 key already used for endpoint secrets.
	masterKeyLength = 32
)

// Hasher holds the pepper. It is built once at boot so the master key is
// validated in one place, and so no call site can forget to pass it — a nil key
// would silently produce an HMAC nobody could reproduce.
type Hasher struct {
	key []byte
}

// NewHasher validates the master key. It reports errs.MissingConfig for any
// length other than 32 bytes: a short key accepted in silence would mean less
// entropy than advertised.
func NewHasher(masterKey []byte) (*Hasher, error) {
	if len(masterKey) != masterKeyLength {
		return nil, errors.Join(errs.MissingConfig,
			fmt.Errorf("apikey: master key must be %d bytes, got %d",
				masterKeyLength, len(masterKey)))
	}
	// Copy so a caller mutating its slice later cannot change our pepper.
	key := make([]byte, masterKeyLength)
	copy(key, masterKey)
	return &Hasher{key: key}, nil
}

// Generate returns a fresh key and its hash. The plaintext is returned once and
// never stored anywhere.
func (h *Hasher) Generate() (plain, hash string, err error) {
	body := make([]byte, 0, keyLength)
	buf := make([]byte, keyLength)

	for len(body) < keyLength {
		if _, err := rand.Read(buf); err != nil {
			return "", "", fmt.Errorf("apikey: read random: %w", err)
		}
		for _, b := range buf {
			if b >= maxUnbiased {
				continue
			}
			body = append(body, alphabet[int(b)%len(alphabet)])
			if len(body) == keyLength {
				break
			}
		}
	}

	plain = Prefix + string(body)
	return plain, h.Hash(plain), nil
}

// Hash is deterministic under a fixed master key: the ingress calls it on an
// incoming key to find the application with a single indexed lookup.
func (h *Hasher) Hash(plain string) string {
	mac := hmac.New(sha256.New, h.key)
	mac.Write([]byte(plain))
	return hex.EncodeToString(mac.Sum(nil))
}
```

- [ ] **Step 4: Rodar os testes para ver passar**

```bash
go test ./internal/apikey/ -v
```

Esperado: PASS em todos. Os dois de 10.000 iterações levam alguns segundos.

- [ ] **Step 5: Verificar que o teste do pepper realmente pega a falha**

Troque temporariamente o corpo de `Hash` por um SHA-256 puro, ignorando `h.key`:

```go
sum := sha256.Sum256([]byte(plain))
return hex.EncodeToString(sum[:])
```

```bash
go test ./internal/apikey/ -run TestThePepperIsActuallyInTheComputation -v
```

Esperado: FAIL com "o pepper não está entrando no cálculo". **Restaure o HMAC e rode de novo até PASS.** Um teste que nunca foi visto vermelho não prova que pega o caso.

- [ ] **Step 6: Commit**

```bash
git add internal/apikey
```

Mensagem:

```
feat: gerar e hashear api key com pepper
```

---

## Task 2: `cmd/adminctl` e o subcomando `genkey`

O esqueleto do binário e o comando que produz a chave mestra — sem ele a Task 3 não tem como ser exercitada.

**Files:**
- Create: `cmd/adminctl/main.go`
- Create: `cmd/adminctl/genkey.go`
- Create: `cmd/adminctl/genkey_test.go`
- Modify: `internal/errs/errs.go`
- Modify: `i18n/errors.pt-BR.json`, `i18n/errors.en.json`, `i18n/errors.es.json`, `i18n/errors.fr.json`

**Interfaces:**
- Consumes: `errs.Error`, `errs.All()`.
- Produces:
  - `errs.AlreadyBootstrapped` (`APP-CFL-001`) e `errs.InvalidArgument` (`APP-VAL-001`)
  - `func genkey(out io.Writer) error`
  - `func run(args []string, out io.Writer) error` em `main.go`

- [ ] **Step 1: Acrescentar as duas constantes de erro**

Em `internal/errs/errs.go`, dentro do bloco `var (...)` que já tem `StorageUnavailable` e companhia:

```go
	// AlreadyBootstrapped: the bootstrap command refuses to run twice. The
	// plaintext key exists only at the moment it is generated, so a second run
	// that silently recreated would orphan the previous application.
	AlreadyBootstrapped = register("APP-CFL-001", TypeCFL)

	// InvalidArgument: a CLI flag carries a value outside the allowed set.
	InvalidArgument = register("APP-VAL-001", TypeVAL)
```

Nenhuma sobrescrita: `TypeCFL` já dá `warn` e 409, `TypeVAL` já dá `warn` e 422, que é o que a spec pede.

- [ ] **Step 2: Rodar o teste de completude para ver falhar**

```bash
go test ./internal/errs/ -run TestEveryCodeHasAMessageInEveryLocale -v
```

Esperado: FAIL, quatro vezes, uma por locale, reclamando de `APP-CFL-001` e `APP-VAL-001`. É o teste da spec 001 fazendo o trabalho dele — código novo sem tradução não compila limpo.

- [ ] **Step 3: Acrescentar as entradas nos quatro locales**

Mantenha a ordem alfabética das chaves em cada arquivo.

`i18n/errors.pt-BR.json`:

```json
  "APP-CFL-001": "O bootstrap já foi executado nesta instalação.",
  "APP-VAL-001": "Valor inválido para um dos argumentos.",
```

`i18n/errors.en.json`:

```json
  "APP-CFL-001": "Bootstrap has already run on this installation.",
  "APP-VAL-001": "Invalid value for one of the arguments.",
```

`i18n/errors.es.json`:

```json
  "APP-CFL-001": "El bootstrap ya se ejecutó en esta instalación.",
  "APP-VAL-001": "Valor inválido para uno de los argumentos.",
```

`i18n/errors.fr.json`:

```json
  "APP-CFL-001": "Le bootstrap a déjà été exécuté sur cette installation.",
  "APP-VAL-001": "Valeur invalide pour l'un des arguments.",
```

- [ ] **Step 4: Rodar o teste de completude para ver passar**

```bash
go test ./internal/errs/ -v
```

Esperado: PASS em todos, incluindo `TestNoOrphanEntriesInAnyLocale`.

- [ ] **Step 5: Escrever o teste do `genkey`**

Crie antes `cmd/adminctl/main.go` com só `package main`, para o red ser sobre símbolos.

`cmd/adminctl/genkey_test.go`:

```go
package main

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenkeyPrintsA32ByteBase64Key(t *testing.T) {
	var out bytes.Buffer
	if err := genkey(&out); err != nil {
		t.Fatalf("genkey() error = %v", err)
	}

	encoded := strings.TrimSpace(out.String())
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("saída não é base64 padrão: %v — %q", err, encoded)
	}
	// 32 bytes é o que apikey.NewHasher exige e o que o AES-256 de
	// endpoints.secret já usa.
	if len(raw) != 32 {
		t.Errorf("chave tem %d bytes, queria 32", len(raw))
	}
}

func TestGenkeyDoesNotRepeat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		var out bytes.Buffer
		if err := genkey(&out); err != nil {
			t.Fatalf("genkey() error = %v", err)
		}
		key := strings.TrimSpace(out.String())
		if seen[key] {
			t.Fatalf("chave mestra repetida na iteração %d", i)
		}
		seen[key] = true
	}
}

func TestRunRejectsAnUnknownSubcommand(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"naoexiste"}, &out); err == nil {
		t.Error("subcomando desconhecido devia falhar")
	}
}

func TestRunWithNoSubcommandFails(t *testing.T) {
	var out bytes.Buffer
	if err := run(nil, &out); err == nil {
		t.Error("sem subcomando devia falhar")
	}
}
```

- [ ] **Step 6: Rodar para ver falhar**

```bash
go test ./cmd/adminctl/ -v
```

Esperado: FAIL na compilação, `undefined: genkey`, `undefined: run`.

- [ ] **Step 7: Implementar o `genkey`**

`cmd/adminctl/genkey.go`:

```go
package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// masterKeyBytes matches what apikey.NewHasher requires and what the AES-256
// key for endpoint secrets already uses.
const masterKeyBytes = 32

// genkey prints a master key. It touches neither the database nor any file:
// whoever runs it decides where the key is going to live.
func genkey(out io.Writer) error {
	key := make([]byte, masterKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("genkey: read random: %w", err)
	}
	_, err := fmt.Fprintln(out, base64.StdEncoding.EncodeToString(key))
	return err
}
```

- [ ] **Step 8: Implementar o despacho**

`cmd/adminctl/main.go`:

```go
// Command adminctl carries the operational tasks that have no place in an HTTP
// surface: creating the first tenant, and minting the master key it needs.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/victorzix/vhook/internal/errs"
)

const usage = `adminctl — operational commands for vhook

  genkey      print a fresh VHOOK_MASTER_KEY
  bootstrap   create the first organization and application
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		// The code is what the operator reports; the wrapped detail is what
		// they read. Neither ever carries the master key or the api key.
		var registered *errs.Error
		if errors.As(err, &registered) {
			fmt.Fprintf(os.Stderr, "error: %s\n%v\n", registered.Code, err)
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(out, usage)
		return errors.New("adminctl: no subcommand given")
	}

	switch args[0] {
	case "genkey":
		return genkey(out)
	case "bootstrap":
		return bootstrap(args[1:], out)
	default:
		_, _ = fmt.Fprint(out, usage)
		return fmt.Errorf("adminctl: unknown subcommand %q", args[0])
	}
}
```

O `case "bootstrap"` não compila ainda — `bootstrap` nasce na Task 3. Crie um arquivo `cmd/adminctl/bootstrap.go` contendo só o suficiente para compilar:

```go
package main

import (
	"errors"
	"io"
)

func bootstrap(args []string, out io.Writer) error {
	return errors.New("adminctl: bootstrap not implemented yet")
}
```

- [ ] **Step 9: Rodar para ver passar**

```bash
go test ./cmd/adminctl/ -v
go build ./...
```

Esperado: PASS nos quatro, build limpo.

- [ ] **Step 10: Exercitar manualmente**

```bash
go run ./cmd/adminctl
go run ./cmd/adminctl genkey
go run ./cmd/adminctl naoexiste
```

Esperado: o texto de uso e exit 1 · uma linha de base64 e exit 0 · o texto de uso e exit 1.

- [ ] **Step 11: Commit**

```bash
git add cmd/adminctl internal/errs i18n
```

Mensagem:

```
feat: criar adminctl com o subcomando genkey
```

---

## Task 3: O subcomando `bootstrap`

**Files:**
- Create: `internal/store/queries/applications.sql`
- Create: `internal/store/sqlc/applications.sql.go` (gerado)
- Create: `cmd/adminctl/config.go`
- Create: `cmd/adminctl/config_test.go`
- Create: `cmd/adminctl/bootstrap_test.go`
- Modify: `cmd/adminctl/bootstrap.go` (substitui o stub da Task 2)
- Modify: `.env.example`
- Modify: `CLAUDE.md` (seção `## Comandos`)

**Interfaces:**
- Consumes: `apikey.NewHasher`, `apikey.Hasher.Generate`, `errs.AlreadyBootstrapped`, `errs.InvalidArgument`, `errs.MissingConfig`, `errs.StorageUnavailable`, `store.NewPool`, `ids.New`, `ids.Encode`.
- Produces: `func bootstrap(args []string, out io.Writer) error` e `func loadConfig() (config, error)`.

**Nota sobre os nomes gerados pelo sqlc.** Os nomes abaixo (`CountOrganizations`, `CreateOrganizationParams`, `CreateApplicationParams`) são **previsão**. Depois de gerar, **leia o arquivo produzido e adapte o código escrito à mão ao que saiu** — nunca o contrário, e nunca edite o gerado. A spec 001 já mostrou que o gerador nomeia coisas de forma diferente da esperada.

- [ ] **Step 1: Escrever as queries**

`internal/store/queries/applications.sql`:

```sql
-- name: CountOrganizations :one
SELECT count(*) FROM organizations;

-- name: CreateOrganization :one
INSERT INTO organizations (id, name)
VALUES ($1, $2)
RETURNING *;

-- name: CreateApplication :one
INSERT INTO applications (id, organization_id, name, api_key_hash, locale, backoff_profile)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;
```

`plan` fica de fora dos parâmetros de propósito: a coluna tem `DEFAULT 'free'` e o `CHECK` só aceita esse valor.

- [ ] **Step 2: Gerar e ler o que saiu**

```bash
go tool sqlc generate
```

Depois **abra `internal/store/sqlc/applications.sql.go`** e anote as assinaturas reais de `CountOrganizations`, `CreateOrganization` e `CreateApplication`, e os nomes dos campos dos structs de params. Use esses nomes nos passos seguintes.

- [ ] **Step 3: Escrever o teste de configuração**

`cmd/adminctl/config_test.go`:

```go
package main

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/victorzix/vhook/internal/errs"
)

func validMasterKey() string {
	return base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func TestLoadConfigReadsBothVariables(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/vhook")
	t.Setenv("VHOOK_MASTER_KEY", validMasterKey())

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.databaseURL == "" {
		t.Error("databaseURL vazio")
	}
	if len(cfg.masterKey) != 32 {
		t.Errorf("masterKey tem %d bytes, queria 32", len(cfg.masterKey))
	}
}

func TestLoadConfigRejectsBadInput(t *testing.T) {
	tests := []struct {
		name      string
		dbURL     string
		masterKey string
		named     string
	}{
		{"sem banco", "", validMasterKey(), "DATABASE_URL"},
		{"sem chave mestra", "postgres://x", "", "VHOOK_MASTER_KEY"},
		{"chave mestra não é base64", "postgres://x", "!!!nao-e-base64!!!", "VHOOK_MASTER_KEY"},
		{"chave mestra curta", "postgres://x", base64.StdEncoding.EncodeToString([]byte("curta")), "VHOOK_MASTER_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", tt.dbURL)
			t.Setenv("VHOOK_MASTER_KEY", tt.masterKey)

			_, err := loadConfig()
			if !errors.Is(err, errs.MissingConfig) {
				t.Fatalf("error = %v, queria errs.MissingConfig", err)
			}
			if !strings.Contains(err.Error(), tt.named) {
				t.Errorf("error = %v, devia nomear %s", err, tt.named)
			}
			// O valor da chave mestra nunca pode aparecer numa mensagem de erro.
			if tt.masterKey != "" && strings.Contains(err.Error(), tt.masterKey) {
				t.Error("o valor de VHOOK_MASTER_KEY vazou na mensagem de erro")
			}
		})
	}
}
```

- [ ] **Step 4: Rodar para ver falhar**

```bash
go test ./cmd/adminctl/ -run TestLoadConfig -v
```

Esperado: FAIL, `undefined: loadConfig`.

- [ ] **Step 5: Implementar a configuração**

`cmd/adminctl/config.go`:

```go
package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/victorzix/vhook/internal/errs"
)

// config is the whole environment surface of adminctl: one address and one
// secret. Everything else the command needs comes from flags, because it is
// behaviour of a single invocation and not of the deployment.
type config struct {
	databaseURL string
	masterKey   []byte
}

func loadConfig() (config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return config{}, errors.Join(errs.MissingConfig,
			errors.New("config: DATABASE_URL is not set"))
	}

	encoded := os.Getenv("VHOOK_MASTER_KEY")
	if encoded == "" {
		return config{}, errors.Join(errs.MissingConfig,
			errors.New("config: VHOOK_MASTER_KEY is not set — run `adminctl genkey`"))
	}

	// Errors never quote the value: the key must not reach a terminal, a log or
	// a CI transcript.
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return config{}, errors.Join(errs.MissingConfig,
			errors.New("config: VHOOK_MASTER_KEY is not valid base64"))
	}
	if len(key) != masterKeyBytes {
		return config{}, errors.Join(errs.MissingConfig,
			fmt.Errorf("config: VHOOK_MASTER_KEY decodes to %d bytes, want %d",
				len(key), masterKeyBytes))
	}

	return config{databaseURL: databaseURL, masterKey: key}, nil
}
```

- [ ] **Step 6: Rodar para ver passar**

```bash
go test ./cmd/adminctl/ -run TestLoadConfig -v
```

Esperado: PASS nos cinco.

- [ ] **Step 7: Escrever os testes do bootstrap**

`cmd/adminctl/bootstrap_test.go`:

```go
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
```

- [ ] **Step 8: Rodar para ver falhar**

```bash
go test ./cmd/adminctl/ -run 'TestBootstrap|TestStored|TestADifferent|TestSecondRun|TestInvalidFlags|TestAFailure|TestConcurrent|TestDefaults' -v
```

Esperado: FAIL — o stub da Task 2 devolve `bootstrap not implemented yet`.

- [ ] **Step 9: Implementar o bootstrap**

Substitua `cmd/adminctl/bootstrap.go` inteiro. **Ajuste os nomes de `sqlc` ao que o Step 2 mostrou.**

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"

	"github.com/victorzix/vhook/internal/apikey"
	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/ids"
	"github.com/victorzix/vhook/internal/store"
	"github.com/victorzix/vhook/internal/store/sqlc"
)

var (
	validLocales  = []string{"pt-BR", "en", "es", "fr"}
	validProfiles = []string{"production", "demo"}
)

const maxNameLength = 200

type bootstrapFlags struct {
	org            string
	app            string
	locale         string
	backoffProfile string
}

func parseBootstrapFlags(args []string) (bootstrapFlags, error) {
	var f bootstrapFlags
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.StringVar(&f.org, "org", "vhook", "organization name")
	fs.StringVar(&f.app, "app", "default", "application name")
	fs.StringVar(&f.locale, "locale", "pt-BR", "one of pt-BR, en, es, fr")
	fs.StringVar(&f.backoffProfile, "backoff-profile", "production", "production or demo")
	if err := fs.Parse(args); err != nil {
		return bootstrapFlags{}, errors.Join(errs.InvalidArgument, err)
	}

	switch {
	case f.org == "" || len(f.org) > maxNameLength:
		return bootstrapFlags{}, errors.Join(errs.InvalidArgument,
			fmt.Errorf("bootstrap: --org must be 1..%d characters", maxNameLength))
	case f.app == "" || len(f.app) > maxNameLength:
		return bootstrapFlags{}, errors.Join(errs.InvalidArgument,
			fmt.Errorf("bootstrap: --app must be 1..%d characters", maxNameLength))
	case !slices.Contains(validLocales, f.locale):
		return bootstrapFlags{}, errors.Join(errs.InvalidArgument,
			fmt.Errorf("bootstrap: --locale must be one of %v", validLocales))
	case !slices.Contains(validProfiles, f.backoffProfile):
		return bootstrapFlags{}, errors.Join(errs.InvalidArgument,
			fmt.Errorf("bootstrap: --backoff-profile must be one of %v", validProfiles))
	}
	return f, nil
}

// bootstrap creates the first organization and application. It refuses to run
// twice: the plaintext key exists only at the moment it is generated, so a
// second run that recreated silently would orphan the previous application and
// leave it unreachable with nobody noticing.
func bootstrap(args []string, out io.Writer) error {
	// Flags first, so a typo never reaches the database.
	f, err := parseBootstrapFlags(args)
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	hasher, err := apikey.NewHasher(cfg.masterKey)
	if err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := store.NewPool(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.Join(errs.StorageUnavailable, fmt.Errorf("bootstrap: begin: %w", err))
	}
	// Rollback after a successful commit is a no-op, so this is safe as the
	// single cleanup path.
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)

	existing, err := q.CountOrganizations(ctx)
	if err != nil {
		return errors.Join(errs.StorageUnavailable, fmt.Errorf("bootstrap: count: %w", err))
	}
	if existing > 0 {
		return errors.Join(errs.AlreadyBootstrapped,
			errors.New("bootstrap: an organization already exists"))
	}

	orgID, err := ids.New()
	if err != nil {
		return fmt.Errorf("bootstrap: new organization id: %w", err)
	}
	appID, err := ids.New()
	if err != nil {
		return fmt.Errorf("bootstrap: new application id: %w", err)
	}

	plain, hash, err := hasher.Generate()
	if err != nil {
		return err
	}

	if _, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		ID:   orgID,
		Name: f.org,
	}); err != nil {
		return errors.Join(errs.StorageUnavailable,
			fmt.Errorf("bootstrap: create organization: %w", err))
	}

	if _, err := q.CreateApplication(ctx, sqlc.CreateApplicationParams{
		ID:             appID,
		OrganizationID: orgID,
		Name:           f.app,
		ApiKeyHash:     hash,
		Locale:         f.locale,
		BackoffProfile: f.backoffProfile,
	}); err != nil {
		return errors.Join(errs.StorageUnavailable,
			fmt.Errorf("bootstrap: create application: %w", err))
	}

	if err := tx.Commit(ctx); err != nil {
		return errors.Join(errs.StorageUnavailable, fmt.Errorf("bootstrap: commit: %w", err))
	}

	// Printed only after the commit: a key shown for a transaction that then
	// failed would send someone chasing a credential that does not exist.
	//
	// The writes discard their error on purpose. errcheck excludes os.Stderr
	// but not a generic io.Writer, and a failed write to stdout is not
	// something this command can act on — the transaction already committed.
	_, _ = fmt.Fprintf(out, "organization  %s  %s\n",
		ids.Encode(ids.Organization, orgID), f.org)
	_, _ = fmt.Fprintf(out, "application   %s  %s\n",
		ids.Encode(ids.Application, appID), f.app)
	_, _ = fmt.Fprintf(out, "              plan=free  locale=%s  backoff_profile=%s\n",
		f.locale, f.backoffProfile)
	_, _ = fmt.Fprintf(out, "api key       %s\n", plain)
	_, _ = fmt.Fprint(out, "              ^ shown once. It cannot be recovered.\n")
	return nil
}
```

- [ ] **Step 10: Rodar para ver passar**

```bash
go test ./cmd/adminctl/ -v
```

Esperado: PASS em todos. Os de integração sobem um Postgres cada e levam dezenas de segundos.

- [ ] **Step 11: Verificar que `-short` pula os de integração**

```bash
go test ./cmd/adminctl/ -short -v
```

Esperado: os de config e de `genkey` passam; os de bootstrap aparecem como SKIP.

- [ ] **Step 12: Acrescentar `VHOOK_MASTER_KEY` ao `.env.example`**

```
# Endereços de infraestrutura. Nenhum comportamento do sistema mora aqui.
DATABASE_URL=postgres://vhook:vhook@localhost:5432/vhook?sslmode=disable
RABBITMQ_URL=amqp://guest:guest@localhost:5672/
VHOOK_HTTP_ADDR=:8080

# Segredo. Gere o seu com `go run ./cmd/adminctl genkey` e NUNCA reuse este.
# Trocar esta chave invalida todas as api keys já emitidas — ver ARCHITECTURE.md §4.33.
VHOOK_MASTER_KEY=
```

- [ ] **Step 13: Verificação manual**

```bash
docker compose up -d postgres rabbitmq
export DATABASE_URL='postgres://vhook:vhook@localhost:5432/vhook?sslmode=disable'
export VHOOK_MASTER_KEY="$(go run ./cmd/adminctl genkey)"

go run ./cmd/api &                 # aplica as migrations, depois pare com kill
go run ./cmd/adminctl bootstrap    # três linhas e a chave
go run ./cmd/adminctl bootstrap    # error: APP-CFL-001, exit 1
go run ./cmd/adminctl bootstrap --locale de   # error: APP-VAL-001, exit 1
```

Depois, o cenário que prova o pepper de fora:

```bash
psql "$DATABASE_URL" -c "SELECT api_key_hash FROM applications"
psql "$DATABASE_URL" -c "DELETE FROM organizations"
export VHOOK_MASTER_KEY="$(go run ./cmd/adminctl genkey)"
go run ./cmd/adminctl bootstrap
psql "$DATABASE_URL" -c "SELECT api_key_hash FROM applications"
```

Esperado: os dois hashes diferentes. Confirme também que `vhook_id('app_…')` com o id impresso encontra a linha.

- [ ] **Step 14: Atualizar a seção de comandos**

Em `CLAUDE.md`, na tabela `## Comandos`, acrescente abaixo de `make run`:

```markdown
| `go run ./cmd/adminctl genkey` | gera uma `VHOOK_MASTER_KEY` |
| `go run ./cmd/adminctl bootstrap` | cria a primeira organização e application, e imprime a api key uma única vez |
```

- [ ] **Step 15: Rodar a suíte inteira**

```bash
go tool sqlc generate && go tool oapi-codegen -config contracts/oapi-codegen.yaml contracts/openapi.yaml && git diff --exit-code
go vet ./...
gofmt -l .
go tool golangci-lint run ./...
go test -shuffle=on ./...
```

Esperado: sem diff, sem saída de `vet`, `gofmt` e lint, PASS em todos os pacotes.

- [ ] **Step 16: Commit**

```bash
git add cmd/adminctl internal/store .env.example CLAUDE.md
```

Mensagem:

```
feat: criar a primeira organização e application
```

Este é o `feat:` que corta a `v0.2.0` no merge do PR de release.

---

## Encerramento da spec

- [ ] **Escrever o `result.md`** com o template de `docs/specs/_template_/result.md`. Divergência e evidência, nunca o que o CHANGELOG já diz.

Candidatos prováveis de divergência, porque o plano os assume sem poder verificar:

- os nomes que o `sqlc` gerou para `CountOrganizations`, `CreateOrganizationParams` e `CreateApplicationParams`, e o tipo de `ApiKeyHash` e `Locale` nos params (Task 3, Step 2);
- se `CountOrganizations` devolve `int64` como esperado;
- o comportamento de `flag.ContinueOnError` quando recebe uma flag desconhecida — ele imprime uso no stderr por conta própria.

- [ ] **Atualizar o índice** em `docs/specs/README.md`: status da 002 para `implementada`.
