# Cadastro de endpoints — Plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Permitir cadastrar o destino de um webhook, com secret cifrado e validação de SSRF, atrás da primeira autenticação de management do projeto.

**Architecture:** Quatro rotas aninhadas sob `/v1/applications/{application_id}/endpoints`. A autenticação e a validação de corpo vêm **do contrato**, pelo middleware do `oapi-codegen`, e não de código escrito à mão por rota. A geração de token sai de `internal/apikey` para `internal/tokens`; a cifra nasce em `internal/secrets`; a checagem de faixas de IP nasce em `internal/dispatch` como função pura, para a spec de disparo reusar a mesma.

**Tech Stack:** Go 1.26 · `go-chi/chi/v5` · `oapi-codegen` + `oapi-codegen/nethttp-middleware` · `jackc/pgx/v5` · `sqlc` · `crypto/aes` + `crypto/cipher` + `crypto/subtle` · `net/netip` · `testcontainers-go`

**Spec:** [`spec.md`](spec.md) · **Release alvo:** `v0.3.0`

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
- **Nenhum segredo em log ou em mensagem de erro.** `VHOOK_ADMIN_TOKEN`, `VHOOK_MASTER_KEY` e o secret do endpoint aparecem só onde a spec manda: o secret na resposta HTTP, os outros em lugar nenhum. Erro nomeia a variável, jamais o valor.
- **Código gerado nunca é editado à mão.** Se a saída não encaixa, o contrato está errado — mas ele foi aprovado, então o mais provável é que a previsão do plano estava errada. **Adapte o código escrito à mão ao gerado e reporte.**
- Teste de integração é marcado com `if testing.Short() { t.Skip(...) }`.
- **`-race` não funciona nesta máquina** (`CGO_ENABLED=0`, sem compilador C). Rode sem. O CI roda `-race` em Ubuntu.
- **`make` não está instalado.** Rode o comando de dentro do alvo.
- **Para ver o red num pacote novo, crie antes o arquivo de implementação contendo só `package X`.**
- **`go mod tidy` depois de todo `go get`, antes de compilar.**
- O linter está fixado como `tool`: `go tool golangci-lint run ./...`. Ele pega `errcheck` e `staticcheck`. Duas armadilhas já vistas: `fmt.Fprintf` para `io.Writer` genérico exige `_, _ =`, e `if f(x) != f(x)` é recusado como `SA4000`.
- **Quem commita é o dono do repositório.** Entregue a mensagem pronta; não rode `git commit`.

---

## Estrutura de arquivos

| Arquivo | Responsabilidade |
|---|---|
| `internal/errs/errs.go` | mais 9 constantes |
| `i18n/errors.{pt-BR,en,es,fr}.json` | mais 9 entradas em cada |
| `internal/tokens/tokens.go` | sorteio base62 sem viés, extraído de `apikey` |
| `internal/apikey/apikey.go` | passa a chamar `tokens` |
| `internal/secrets/secrets.go` | AES-256-GCM, `nonce ‖ ciphertext`, AAD |
| `internal/dispatch/ssrf.go` | `IsForbiddenAddr` puro + `URLGuard` com resolver injetável |
| `internal/httpauth/admin.go` | verificação do token administrativo |
| `internal/store/queries/endpoints.sql` | entrada do `sqlc` |
| `migrations/000003_endpoint_url_unique.{up,down}.sql` | índice único |
| `internal/endpoints/repo.go` | acesso a dados, sem regra |
| `internal/endpoints/service.go` | transação, trava, limite, cifra |
| `internal/endpoints/handler.go` | decodifica, chama o service, serializa |
| `cmd/api/server.go` | monta o router com o validador do contrato |

---

## Task 1: Os nove códigos de erro

Vem primeiro porque as tasks seguintes referenciam as constantes.

**Files:**
- Modify: `internal/errs/errs.go`
- Modify: `i18n/errors.pt-BR.json`, `i18n/errors.en.json`, `i18n/errors.es.json`, `i18n/errors.fr.json`
- Modify: `internal/errs/errs_test.go`

**Interfaces:**
- Produces: `errs.InvalidCredentials`, `errs.MalformedID`, `errs.ApplicationNotFound`, `errs.InvalidEndpointURL`, `errs.ForbiddenAddress`, `errs.UnresolvableHost`, `errs.EndpointNotFound`, `errs.DuplicateEndpoint`, `errs.EndpointLimit`

- [ ] **Step 1: Acrescentar as constantes**

Em `internal/errs/errs.go`, no bloco `var (...)` que já tem `StorageUnavailable`:

```go
	// InvalidCredentials never distinguishes a missing token from a wrong one:
	// telling them apart would confirm to an attacker that the envelope is right.
	InvalidCredentials = register("AUT-CRD-001", TypeCRD)

	// MalformedID is the code spec 001 deferred, saying it would be minted by
	// the first route that takes an identifier in the path. This is that route.
	MalformedID = register("SYS-VAL-001", TypeVAL)

	ApplicationNotFound = register("APP-NFD-001", TypeNFD)

	InvalidEndpointURL = register("EPT-VAL-001", TypeVAL)
	ForbiddenAddress   = register("EPT-VAL-002", TypeVAL)
	UnresolvableHost   = register("EPT-VAL-003", TypeVAL)

	// EndpointNotFound also answers "exists, but belongs to another tenant":
	// a 403 there would confirm the resource exists.
	EndpointNotFound = register("EPT-NFD-001", TypeNFD)

	DuplicateEndpoint = register("EPT-CFL-001", TypeCFL)

	// EndpointLimit overrides LMT's 429: 429 promises that retrying later
	// works, and a capacity quota does not free up with time.
	EndpointLimit = register("RTL-LMT-001", TypeLMT, withStatus(http.StatusForbidden))
)
```

- [ ] **Step 2: Rodar o teste de completude para ver falhar**

```bash
go test ./internal/errs/ -run TestEveryCodeHasAMessageInEveryLocale -v
```

Esperado: FAIL 36 vezes — nove códigos × quatro locales. É a barreira da spec 001 fazendo o trabalho dela.

- [ ] **Step 3: Acrescentar `EndpointLimit` ao teste de sobrescritas**

Em `internal/errs/errs_test.go`, na tabela de `TestDeclaredOverrides`:

```go
		{errs.EndpointLimit, errs.LevelWarn, http.StatusForbidden},
```

Sem isso, alguém "corrige" o 403 para o 429 padrão do tipo e nenhum teste reclama.

- [ ] **Step 4: Acrescentar as entradas nos quatro locales**

Ordem alfabética das chaves em cada arquivo.

`i18n/errors.pt-BR.json`:

```json
  "APP-NFD-001": "Application não encontrada.",
  "AUT-CRD-001": "Credencial ausente ou inválida.",
  "EPT-CFL-001": "Já existe um endpoint com essa URL nesta application.",
  "EPT-NFD-001": "Endpoint não encontrado.",
  "EPT-VAL-001": "URL inválida. O endereço precisa usar https.",
  "EPT-VAL-002": "A URL aponta para um endereço de rede não permitido.",
  "EPT-VAL-003": "Não foi possível resolver o domínio da URL.",
  "RTL-LMT-001": "Limite de endpoints do plano atingido.",
  "SYS-VAL-001": "Identificador malformado.",
```

`i18n/errors.en.json`:

```json
  "APP-NFD-001": "Application not found.",
  "AUT-CRD-001": "Missing or invalid credential.",
  "EPT-CFL-001": "An endpoint with this URL already exists in this application.",
  "EPT-NFD-001": "Endpoint not found.",
  "EPT-VAL-001": "Invalid URL. The address must use https.",
  "EPT-VAL-002": "The URL points to a network address that is not allowed.",
  "EPT-VAL-003": "Could not resolve the URL's domain.",
  "RTL-LMT-001": "Plan endpoint limit reached.",
  "SYS-VAL-001": "Malformed identifier.",
```

`i18n/errors.es.json`:

```json
  "APP-NFD-001": "Application no encontrada.",
  "AUT-CRD-001": "Credencial ausente o inválida.",
  "EPT-CFL-001": "Ya existe un endpoint con esa URL en esta application.",
  "EPT-NFD-001": "Endpoint no encontrado.",
  "EPT-VAL-001": "URL inválida. La dirección debe usar https.",
  "EPT-VAL-002": "La URL apunta a una dirección de red no permitida.",
  "EPT-VAL-003": "No se pudo resolver el dominio de la URL.",
  "RTL-LMT-001": "Límite de endpoints del plan alcanzado.",
  "SYS-VAL-001": "Identificador malformado.",
```

`i18n/errors.fr.json`:

```json
  "APP-NFD-001": "Application introuvable.",
  "AUT-CRD-001": "Identifiant absent ou invalide.",
  "EPT-CFL-001": "Un endpoint avec cette URL existe déjà dans cette application.",
  "EPT-NFD-001": "Endpoint introuvable.",
  "EPT-VAL-001": "URL invalide. L'adresse doit utiliser https.",
  "EPT-VAL-002": "L'URL pointe vers une adresse réseau non autorisée.",
  "EPT-VAL-003": "Impossible de résoudre le domaine de l'URL.",
  "RTL-LMT-001": "Limite d'endpoints du forfait atteinte.",
  "SYS-VAL-001": "Identifiant mal formé.",
```

- [ ] **Step 5: Rodar para ver passar**

```bash
go test ./internal/errs/ -v
```

Esperado: PASS, com o subteste `RTL-LMT-001` visível em `TestDeclaredOverrides`.

- [ ] **Step 6: Commit**

```bash
git add internal/errs i18n
```

```
feat: registrar os erros de cadastro de endpoint
```

---

## Task 2: `internal/tokens` — extrair o gerador

`internal/apikey` gera token base62; o secret do endpoint precisa do mesmo. Usar um pacote chamado `apikey` para gerar secret de endpoint faria o nome mentir.

**Files:**
- Create: `internal/tokens/tokens.go`, `internal/tokens/tokens_test.go`
- Modify: `internal/apikey/apikey.go`, `internal/apikey/apikey_test.go`

**Interfaces:**
- Produces: `tokens.Alphabet`, `func tokens.Random(prefix string, n int) (string, error)`

- [ ] **Step 1: Escrever o teste**

Stub `internal/tokens/tokens.go` com só `package tokens` primeiro.

`internal/tokens/tokens_test.go`:

```go
package tokens_test

import (
	"strings"
	"testing"

	"github.com/victorzix/vhook/internal/tokens"
)

func TestRandomHasThePrefixAndLength(t *testing.T) {
	got, err := tokens.Random("tst_", 43)
	if err != nil {
		t.Fatalf("Random() error = %v", err)
	}
	if !strings.HasPrefix(got, "tst_") {
		t.Errorf("token = %q, queria prefixo tst_", got)
	}
	if body := strings.TrimPrefix(got, "tst_"); len(body) != 43 {
		t.Errorf("corpo tem %d caracteres, queria 43", len(body))
	}
}

func TestRandomWorksWithoutAPrefix(t *testing.T) {
	got, err := tokens.Random("", 10)
	if err != nil {
		t.Fatalf("Random() error = %v", err)
	}
	if len(got) != 10 {
		t.Errorf("len = %d, want 10", len(got))
	}
}

func TestRandomOnlyUsesTheAlphabet(t *testing.T) {
	got, err := tokens.Random("", 200)
	if err != nil {
		t.Fatalf("Random() error = %v", err)
	}
	for i, c := range got {
		if !strings.ContainsRune(tokens.Alphabet, c) {
			t.Errorf("caractere %d é %q, fora do alfabeto", i, c)
		}
	}
}

func TestRandomDoesNotRepeat(t *testing.T) {
	seen := make(map[string]bool, 10000)
	for i := 0; i < 10000; i++ {
		got, err := tokens.Random("", 43)
		if err != nil {
			t.Fatalf("Random() error = %v", err)
		}
		if seen[got] {
			t.Fatalf("token repetido na iteração %d", i)
		}
		seen[got] = true
	}
}

// Pega o viés de módulo: uma implementação com `b % 62` sobre bytes crus
// favoreceria os primeiros caracteres, e nenhum outro teste notaria.
func TestRandomUsesTheWholeAlphabet(t *testing.T) {
	seen := map[rune]bool{}
	for i := 0; i < 10000; i++ {
		got, err := tokens.Random("", 43)
		if err != nil {
			t.Fatalf("Random() error = %v", err)
		}
		for _, c := range got {
			seen[c] = true
		}
	}
	for _, c := range tokens.Alphabet {
		if !seen[c] {
			t.Errorf("o caractere %q nunca foi sorteado em 430.000 posições", c)
		}
	}
}

func TestRandomRejectsNonPositiveLength(t *testing.T) {
	for _, n := range []int{0, -1} {
		if _, err := tokens.Random("", n); err == nil {
			t.Errorf("Random(%d) devia falhar", n)
		}
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

```bash
go test ./internal/tokens/ -v
```

Esperado: FAIL na compilação, `undefined: tokens.Random`.

- [ ] **Step 3: Implementar**

`internal/tokens/tokens.go`:

```go
// Package tokens draws random opaque tokens. It exists apart from the
// credential packages that use it because more than one of them needs the same
// unbiased draw, and a package named after one credential would lie about the
// others.
package tokens

import (
	"crypto/rand"
	"errors"
	"fmt"
)

// Alphabet is Base62: no + or /, which break in URLs and in badly quoted
// environment variables.
const Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// maxUnbiased is the largest multiple of 62 that fits in a byte (62 × 4).
// Bytes at or above it are discarded rather than folded with %, which would
// make the first characters of the alphabet more likely.
const maxUnbiased = 248

// Random returns prefix followed by n characters drawn uniformly from Alphabet.
func Random(prefix string, n int) (string, error) {
	if n <= 0 {
		return "", errors.New("tokens: length must be positive")
	}

	body := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(body) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("tokens: read random: %w", err)
		}
		for _, b := range buf {
			if b >= maxUnbiased {
				continue
			}
			body = append(body, Alphabet[int(b)%len(Alphabet)])
			if len(body) == n {
				break
			}
		}
	}
	return prefix + string(body), nil
}
```

- [ ] **Step 4: Rodar para ver passar**

```bash
go test ./internal/tokens/ -v
```

Esperado: PASS nos seis.

- [ ] **Step 5: Fazer `apikey` usar `tokens`**

Em `internal/apikey/apikey.go`: remova `alphabet` e `maxUnbiased`, remova o import de `crypto/rand`, e troque o corpo de `Generate`:

```go
func (h *Hasher) Generate() (plain, hash string, err error) {
	plain, err = tokens.Random(Prefix, keyLength)
	if err != nil {
		return "", "", err
	}
	return plain, h.Hash(plain), nil
}
```

Acrescente o import `"github.com/victorzix/vhook/internal/tokens"`.

Em `internal/apikey/apikey_test.go`, troque a constante local `base62` por `tokens.Alphabet` e **remova** `TestGeneratedKeysUseTheWholeAlphabet` — o teste de distribuição migrou para `tokens`, e mantê-lo nos dois lugares é 20 segundos gastos duas vezes provando a mesma coisa.

- [ ] **Step 6: Rodar a suíte de `apikey` para ver que nada quebrou**

```bash
go test ./internal/apikey/ -v
```

Esperado: PASS. Em especial `TestGenerateProducesTheDocumentedFormat` e `TestThePepperIsActuallyInTheComputation` — a refatoração não pode ter mudado nem o formato nem o hash.

- [ ] **Step 7: Commit**

```bash
git add internal/tokens internal/apikey
```

```
refactor: extrair o gerador de token de apikey
```

---

## Task 3: `internal/secrets` — AES-GCM com AAD

**Files:**
- Create: `internal/secrets/secrets.go`, `internal/secrets/secrets_test.go`

**Interfaces:**
- Consumes: `errs.MissingConfig`
- Produces:
  - `func secrets.NewCipher(masterKey []byte) (*secrets.Cipher, error)`
  - `func (c *Cipher) Seal(plaintext, aad []byte) ([]byte, error)`
  - `func (c *Cipher) Open(blob, aad []byte) ([]byte, error)`
  - `var secrets.ErrDecrypt error`

- [ ] **Step 1: Escrever os testes**

Stub `internal/secrets/secrets.go` com só `package secrets`.

`internal/secrets/secrets_test.go`:

```go
package secrets_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/secrets"
)

var (
	masterA = []byte("0123456789abcdef0123456789abcdef")
	masterB = []byte("fedcba9876543210fedcba9876543210")
)

func newCipher(t *testing.T, master []byte) *secrets.Cipher {
	t.Helper()
	c, err := secrets.NewCipher(master)
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	return c
}

func TestSealThenOpenRoundTrips(t *testing.T) {
	c := newCipher(t, masterA)
	plaintext := []byte("whsec_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H")
	aad := []byte("ept_01J4PMX3R0E008000000000003")

	blob, err := c.Seal(plaintext, aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	got, err := c.Open(blob, aad)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("Open() = %q, want %q", got, plaintext)
	}
}

func TestCiphertextDoesNotContainThePlaintext(t *testing.T) {
	c := newCipher(t, masterA)
	plaintext := []byte("whsec_umsegredoquenaopodevazar")

	blob, err := c.Seal(plaintext, []byte("ept_1"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if bytes.Contains(blob, plaintext) {
		t.Error("o texto em claro aparece dentro do blob cifrado")
	}
}

// Duas cifragens do mesmo texto têm de diferir: nonce reutilizado em AES-GCM
// é falha catastrófica, não estética — dois textos sob o mesmo nonce vazam o
// XOR entre eles.
func TestSealUsesAFreshNonceEveryTime(t *testing.T) {
	c := newCipher(t, masterA)
	plaintext := []byte("mesmo texto")
	aad := []byte("ept_1")

	first, err := c.Seal(plaintext, aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	second, err := c.Seal(plaintext, aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("duas cifragens produziram o mesmo blob: o nonce não está variando")
	}
}

// O teste central desta task. Sem ele, uma implementação que passasse nil como
// AAD passaria em todos os outros — e o ganho de vincular o ciphertext ao
// endpoint estaria perdido em silêncio.
func TestOpenWithADifferentAADFails(t *testing.T) {
	c := newCipher(t, masterA)

	blob, err := c.Seal([]byte("segredo"), []byte("ept_01J4PMX3R0E008000000000003"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}

	got, err := c.Open(blob, []byte("ept_01J4PMX3R0E008000000000009"))
	if !errors.Is(err, secrets.ErrDecrypt) {
		t.Fatalf("error = %v, queria secrets.ErrDecrypt — o AAD não está no cálculo", err)
	}
	if got != nil {
		t.Error("Open() devolveu dado apesar do erro")
	}
}

func TestOpenWithADifferentMasterKeyFails(t *testing.T) {
	blob, err := newCipher(t, masterA).Seal([]byte("segredo"), []byte("ept_1"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if _, err := newCipher(t, masterB).Open(blob, []byte("ept_1")); !errors.Is(err, secrets.ErrDecrypt) {
		t.Errorf("error = %v, queria secrets.ErrDecrypt", err)
	}
}

func TestOpenRejectsATamperedBlob(t *testing.T) {
	c := newCipher(t, masterA)
	blob, err := c.Seal([]byte("segredo"), []byte("ept_1"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}

	tampered := bytes.Clone(blob)
	tampered[len(tampered)-1] ^= 0x01

	if _, err := c.Open(tampered, []byte("ept_1")); !errors.Is(err, secrets.ErrDecrypt) {
		t.Errorf("error = %v, queria secrets.ErrDecrypt", err)
	}
}

func TestOpenRejectsABlobShorterThanTheNonce(t *testing.T) {
	c := newCipher(t, masterA)
	if _, err := c.Open([]byte{1, 2, 3}, []byte("ept_1")); !errors.Is(err, secrets.ErrDecrypt) {
		t.Errorf("error = %v, queria secrets.ErrDecrypt", err)
	}
}

func TestNewCipherRejectsBadMasterKeys(t *testing.T) {
	for _, tt := range []struct {
		name   string
		master []byte
	}{
		{"nil", nil},
		{"vazia", []byte{}},
		{"curta", []byte("curta")},
		{"longa", []byte(strings.Repeat("x", 64))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := secrets.NewCipher(tt.master); !errors.Is(err, errs.MissingConfig) {
				t.Errorf("error = %v, queria errs.MissingConfig", err)
			}
		})
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

```bash
go test ./internal/secrets/ -v
```

Esperado: FAIL na compilação, `undefined: secrets.NewCipher`.

- [ ] **Step 3: Implementar**

`internal/secrets/secrets.go`:

```go
// Package secrets encrypts values that the system must be able to read back —
// unlike credentials it only needs to verify, which are hashed instead. The
// endpoint secret is the case: vhook needs it in the clear to sign outgoing
// deliveries, so hashing it would make delivery impossible. See §4.12.
//
// Named secrets and not crypto so it does not shadow the standard library
// package at call sites.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/victorzix/vhook/internal/errs"
)

// masterKeyLength selects AES-256.
const masterKeyLength = 32

// ErrDecrypt covers every failure to recover a plaintext: wrong key, wrong
// AAD, tampered bytes, truncated blob. They are deliberately indistinguishable
// — telling them apart would help an attacker probing which part is wrong.
var ErrDecrypt = errors.New("secrets: cannot decrypt")

// Cipher seals and opens values under the master key.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher validates the master key once, at boot, so no call site can pass a
// nil key and silently produce output nobody can read back.
func NewCipher(masterKey []byte) (*Cipher, error) {
	if len(masterKey) != masterKeyLength {
		return nil, errors.Join(errs.MissingConfig,
			fmt.Errorf("secrets: master key must be %d bytes, got %d",
				masterKeyLength, len(masterKey)))
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("secrets: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: new gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Seal returns nonce ‖ ciphertext. The nonce is not secret and is stored
// alongside; what matters is that it is never reused under the same key, so it
// comes from crypto/rand and is never derived or counted.
//
// aad is authenticated but not encrypted. Passing the owning row's identifier
// makes a blob moved to another row fail to open, instead of opening fine.
func (c *Cipher) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: read nonce: %w", err)
	}
	// Sealing into nonce appends the ciphertext to it, giving nonce ‖ ct.
	return c.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Open reverses Seal. Every failure is ErrDecrypt.
func (c *Cipher) Open(blob, aad []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(blob) < n {
		return nil, ErrDecrypt
	}
	plaintext, err := c.aead.Open(nil, blob[:n], blob[n:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}
```

- [ ] **Step 4: Rodar para ver passar**

```bash
go test ./internal/secrets/ -v
```

Esperado: PASS em todos.

- [ ] **Step 5: Verificar que o teste do AAD pega a falha**

Troque temporariamente as duas chamadas do AEAD para passar `nil` no lugar de `aad`:

```go
return c.aead.Seal(nonce, nonce, plaintext, nil), nil
// e
plaintext, err := c.aead.Open(nil, blob[:n], blob[n:], nil)
```

```bash
go test ./internal/secrets/ -run TestOpenWithADifferentAADFails -v
```

Esperado: FAIL com "o AAD não está no cálculo". **Restaure `aad` nas duas e rode até PASS.** Um teste que nunca foi visto vermelho não prova que pega o caso.

- [ ] **Step 6: Commit**

```bash
git add internal/secrets
```

```
feat: cifrar segredo com aes-gcm vinculado ao dono
```

---

## Task 4: `internal/dispatch` — a checagem de SSRF

**Files:**
- Create: `internal/dispatch/ssrf.go`, `internal/dispatch/ssrf_test.go`

**Interfaces:**
- Consumes: `errs.InvalidEndpointURL`, `errs.ForbiddenAddress`, `errs.UnresolvableHost`
- Produces:
  - `func dispatch.IsForbiddenAddr(addr netip.Addr) bool`
  - `type dispatch.Resolver interface { LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) }`
  - `func dispatch.NewURLGuard(r Resolver, allowlist []string) *URLGuard`
  - `func (g *URLGuard) Validate(ctx context.Context, rawURL string) error`

- [ ] **Step 1: Escrever os testes**

Stub `internal/dispatch/ssrf.go` com só `package dispatch`.

`internal/dispatch/ssrf_test.go`:

```go
package dispatch_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/victorzix/vhook/internal/dispatch"
	"github.com/victorzix/vhook/internal/errs"
)

func TestIsForbiddenAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
		why  string
	}{
		{"10.0.0.1", true, "RFC1918"},
		{"172.16.0.1", true, "RFC1918"},
		{"172.31.255.255", true, "RFC1918"},
		{"192.168.1.1", true, "RFC1918"},
		{"127.0.0.1", true, "loopback"},
		{"169.254.169.254", true, "link-local: metadados de cloud"},
		{"100.64.0.1", true, "CGNAT"},
		{"0.0.0.0", true, "não roteável"},
		{"::1", true, "loopback IPv6"},
		{"fc00::1", true, "ULA IPv6"},
		{"fe80::1", true, "link-local IPv6"},
		// O driblador clássico: lista que só olha IPv4 deixa passar isto,
		// e a conexão acaba em 10.0.0.1.
		{"::ffff:10.0.0.1", true, "IPv4 privado mapeado em IPv6"},
		{"::ffff:169.254.169.254", true, "link-local mapeado em IPv6"},
		// Estes dois são os que realmente dependem do Unmap() da nossa
		// implementação. Os predicados do net/netip — IsPrivate, IsLoopback,
		// IsLinkLocalUnicast — já desmapeiam 4-em-6 por dentro, então os dois
		// casos acima passariam mesmo sem a linha. Já IsUnspecified não
		// desmapeia, e os prefixos de CGNAT e 0.0.0.0/8 estão atrás de uma
		// guarda Is4(), falsa para 4-em-6. Sem estes casos, apagar o Unmap()
		// num refactor não produziria nenhum teste vermelho.
		{"::ffff:100.64.0.1", true, "CGNAT mapeado: a guarda Is4() falha sem Unmap"},
		{"::ffff:0.0.0.0", true, "não roteável mapeado: IsUnspecified não desmapeia"},

		{"1.1.1.1", false, "público"},
		{"172.32.0.1", false, "fora do bloco RFC1918"},
		{"100.128.0.1", false, "fora do CGNAT"},
		{"2606:4700::1111", false, "público IPv6"},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			addr, err := netip.ParseAddr(tt.addr)
			if err != nil {
				t.Fatalf("ParseAddr: %v", err)
			}
			if got := dispatch.IsForbiddenAddr(addr); got != tt.want {
				t.Errorf("IsForbiddenAddr(%s) = %v, want %v — %s",
					tt.addr, got, tt.want, tt.why)
			}
		})
	}
}

// fakeResolver devolve o que o teste mandar, sem tocar a rede.
type fakeResolver struct {
	byHost map[string][]string
	err    error
}

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if f.err != nil {
		return nil, f.err
	}
	raw, ok := f.byHost[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	out := make([]netip.Addr, 0, len(raw))
	for _, s := range raw {
		out = append(out, netip.MustParseAddr(s))
	}
	return out, nil
}

func guard(t *testing.T, hosts map[string][]string, allowlist ...string) *dispatch.URLGuard {
	t.Helper()
	return dispatch.NewURLGuard(fakeResolver{byHost: hosts}, allowlist)
}

func TestValidateAcceptsAPublicHTTPSURL(t *testing.T) {
	g := guard(t, map[string][]string{"api.cliente.com": {"1.1.1.1"}})
	if err := g.Validate(context.Background(), "https://api.cliente.com/hooks"); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestValidateRejectsNonHTTPS(t *testing.T) {
	g := guard(t, map[string][]string{"api.cliente.com": {"1.1.1.1"}})
	for _, raw := range []string{
		"http://api.cliente.com/hooks",
		"ftp://api.cliente.com/hooks",
		"api.cliente.com/hooks",
		"",
		"https://",
		"não é uma url",
	} {
		t.Run(raw, func(t *testing.T) {
			if err := g.Validate(context.Background(), raw); !errors.Is(err, errs.InvalidEndpointURL) {
				t.Errorf("error = %v, queria errs.InvalidEndpointURL", err)
			}
		})
	}
}

func TestValidateRejectsAForbiddenAddress(t *testing.T) {
	g := guard(t, map[string][]string{"interno.exemplo.com": {"10.0.0.1"}})
	err := g.Validate(context.Background(), "https://interno.exemplo.com/hooks")
	if !errors.Is(err, errs.ForbiddenAddress) {
		t.Errorf("error = %v, queria errs.ForbiddenAddress", err)
	}
}

// Um IP público entre vários não salva: basta um proibido para recusar.
func TestValidateRejectsWhenAnyResolvedAddressIsForbidden(t *testing.T) {
	g := guard(t, map[string][]string{"misto.exemplo.com": {"1.1.1.1", "10.0.0.1"}})
	err := g.Validate(context.Background(), "https://misto.exemplo.com/hooks")
	if !errors.Is(err, errs.ForbiddenAddress) {
		t.Errorf("error = %v, queria errs.ForbiddenAddress", err)
	}
}

func TestValidateRejectsAnUnresolvableHost(t *testing.T) {
	g := guard(t, map[string][]string{})
	err := g.Validate(context.Background(), "https://naoexiste.exemplo.com/hooks")
	if !errors.Is(err, errs.UnresolvableHost) {
		t.Errorf("error = %v, queria errs.UnresolvableHost", err)
	}
}

func TestValidateRejectsAHostThatResolvesToNothing(t *testing.T) {
	g := guard(t, map[string][]string{"vazio.exemplo.com": {}})
	err := g.Validate(context.Background(), "https://vazio.exemplo.com/hooks")
	if !errors.Is(err, errs.UnresolvableHost) {
		t.Errorf("error = %v, queria errs.UnresolvableHost", err)
	}
}

// O sink roda no compose e resolve para um IP privado. A allowlist existe
// exatamente para ele.
func TestAllowlistedHostSkipsTheAddressCheck(t *testing.T) {
	g := guard(t, map[string][]string{"sink": {"172.18.0.5"}}, "sink")
	if err := g.Validate(context.Background(), "https://sink/hooks"); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

// Mas a allowlist não perdoa http: ela dispensa a checagem de faixa, não o TLS.
func TestAllowlistedHostStillRequiresHTTPS(t *testing.T) {
	g := guard(t, map[string][]string{"sink": {"172.18.0.5"}}, "sink")
	if err := g.Validate(context.Background(), "http://sink/hooks"); !errors.Is(err, errs.InvalidEndpointURL) {
		t.Errorf("error = %v, queria errs.InvalidEndpointURL", err)
	}
}

func TestAllowlistDoesNotMatchBySuffix(t *testing.T) {
	// "sink" na allowlist não pode liberar "evil-sink" nem "sink.evil.com".
	g := guard(t, map[string][]string{
		"evil-sink":    {"10.0.0.1"},
		"sink.evil.com": {"10.0.0.1"},
	}, "sink")
	for _, host := range []string{"evil-sink", "sink.evil.com"} {
		t.Run(host, func(t *testing.T) {
			err := g.Validate(context.Background(), "https://"+host+"/hooks")
			if !errors.Is(err, errs.ForbiddenAddress) {
				t.Errorf("error = %v, queria errs.ForbiddenAddress", err)
			}
		})
	}
}

// Um IP literal na URL não pode pular a checagem por não ter o que resolver.
func TestValidateRejectsAForbiddenIPLiteral(t *testing.T) {
	g := guard(t, map[string][]string{"169.254.169.254": {"169.254.169.254"}})
	err := g.Validate(context.Background(), "https://169.254.169.254/latest/meta-data/")
	if !errors.Is(err, errs.ForbiddenAddress) {
		t.Errorf("error = %v, queria errs.ForbiddenAddress", err)
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

```bash
go test ./internal/dispatch/ -v
```

Esperado: FAIL na compilação, `undefined: dispatch.IsForbiddenAddr`.

- [ ] **Step 3: Implementar**

`internal/dispatch/ssrf.go`:

```go
// Package dispatch owns everything about reaching a customer's endpoint: the
// HTTP client, the signature, the timeout, and the guard that keeps the worker
// from being turned into a scanner of our own network.
//
// This file holds only the guard. It lands here, and not in the package that
// registers endpoints, because the registration check and the delivery-time
// check must be the same code: two implementations of one rule drift on their
// own, and drifting here means one path allowing what the other blocks.
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"time"

	"github.com/victorzix/vhook/internal/errs"
)

// resolveTimeout bounds DNS. A host that takes longer than this is treated as
// unresolvable: registration must not hang on someone else's nameserver.
const resolveTimeout = 3 * time.Second

// Ranges the standard library has no predicate for.
var (
	cgnat        = netip.MustParsePrefix("100.64.0.0/10")
	unspecifiedV4 = netip.MustParsePrefix("0.0.0.0/8")
)

// IsForbiddenAddr reports whether an endpoint must never be allowed to reach
// addr. It is pure and comparable, so the delivery-time dialer can call the
// very same function on the address a connection is about to use.
func IsForbiddenAddr(addr netip.Addr) bool {
	// Unmap first. Without this, ::ffff:10.0.0.1 passes every IPv4 predicate
	// while connecting to 10.0.0.1 — the classic way around a v4-only list.
	addr = addr.Unmap()

	switch {
	case !addr.IsValid():
		return true
	case addr.IsLoopback(): // 127/8, ::1
		return true
	case addr.IsPrivate(): // RFC1918, fc00::/7
		return true
	case addr.IsLinkLocalUnicast(): // 169.254/16, fe80::/10
		return true
	case addr.IsLinkLocalMulticast(), addr.IsInterfaceLocalMulticast(), addr.IsMulticast():
		return true
	case addr.IsUnspecified():
		return true
	case addr.Is4() && cgnat.Contains(addr):
		return true
	case addr.Is4() && unspecifiedV4.Contains(addr):
		return true
	default:
		return false
	}
}

// Resolver is the slice of net.Resolver this package needs. Declared here, by
// the consumer, so tests can answer without touching the network.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// URLGuard validates a destination URL before it is stored.
type URLGuard struct {
	resolver  Resolver
	allowlist map[string]bool
}

// NewURLGuard builds the guard. Hosts in allowlist skip the address check —
// and only that check. The case it exists for is the sink in the compose
// network, which resolves to a private address on purpose.
func NewURLGuard(resolver Resolver, allowlist []string) *URLGuard {
	allowed := make(map[string]bool, len(allowlist))
	for _, host := range allowlist {
		if host != "" {
			allowed[host] = true
		}
	}
	return &URLGuard{resolver: resolver, allowlist: allowed}
}

// Validate checks scheme, resolves the host, and rejects if any resolved
// address is forbidden.
//
// This is defence in depth, NOT the guarantee. DNS can change between
// registration and delivery, so the load-bearing check is the dialer's, on the
// address the connection actually uses. See spec 003.
func (g *URLGuard) Validate(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.Join(errs.InvalidEndpointURL, fmt.Errorf("dispatch: parse url: %w", err))
	}
	if parsed.Scheme != "https" {
		return errors.Join(errs.InvalidEndpointURL,
			errors.New("dispatch: endpoint url must use https"))
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.Join(errs.InvalidEndpointURL, errors.New("dispatch: url has no host"))
	}

	// Exact match only. Suffix matching would let "evil-sink" through on an
	// allowlist meant for "sink".
	if g.allowlist[host] {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	addrs, err := g.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return errors.Join(errs.UnresolvableHost, fmt.Errorf("dispatch: resolve %q: %w", host, err))
	}
	if len(addrs) == 0 {
		return errors.Join(errs.UnresolvableHost,
			fmt.Errorf("dispatch: %q resolved to no addresses", host))
	}

	// One public address among several does not save it: any forbidden
	// address means the destination can be reached inside our network.
	for _, addr := range addrs {
		if IsForbiddenAddr(addr) {
			return errors.Join(errs.ForbiddenAddress,
				fmt.Errorf("dispatch: %q resolves to a forbidden address", host))
		}
	}
	return nil
}
```

- [ ] **Step 4: Rodar para ver passar**

```bash
go test ./internal/dispatch/ -v
```

Esperado: PASS em todos, com os 17 subtestes de `TestIsForbiddenAddr` visíveis.

- [ ] **Step 5: Verificar que o `Unmap` é o que pega o IPv4 mapeado**

Remova temporariamente a linha `addr = addr.Unmap()`:

```bash
go test ./internal/dispatch/ -run 'TestIsForbiddenAddr/::ffff' -v
```

Esperado: FAIL em **`::ffff:100.64.0.1` e `::ffff:0.0.0.0`**, e PASS nos outros dois casos mapeados. **Restaure a linha e rode até PASS.**

**Por que só esses dois falham, e por que isso importa.** Os predicados do `net/netip` — `IsPrivate`, `IsLoopback`, `IsLinkLocalUnicast`, `IsMulticast` — abrem todos com `if ip.Is4In6() { ip = ip.Unmap() }`. Então `::ffff:10.0.0.1` e `::ffff:169.254.169.254` são bloqueados pela stdlib mesmo sem a nossa linha.

Quem depende dela é o resto: `IsUnspecified` **não** desmapeia, e os dois prefixos explícitos — CGNAT e `0.0.0.0/8` — estão atrás de uma guarda `addr.Is4()`, que é falsa para um endereço 4-em-6.

Isso quer dizer que uma tabela de teste sem esses dois casos **não protege a linha**: um refactor a apaga e tudo segue verde. É o modo de falha exato que este passo existe para impedir, e ele só é reproduzível com os casos certos.

- [ ] **Step 6: Commit**

```bash
git add internal/dispatch
```

```
feat: bloquear destino em faixa de rede interna
```

---

## Task 5: Contrato gerado, validação e autenticação

A task de maior risco do plano: os nomes gerados e a API do middleware são **previsão**, não fato.

**Files:**
- Modify: `contracts/oapi-codegen.yaml`
- Create: `internal/httpauth/admin.go`, `internal/httpauth/admin_test.go`
- Modify: `internal/openapi/openapi.gen.go` (gerado)
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces:
  - `func httpauth.CheckAdminToken(r *http.Request, expected string) error`
  - `func httpauth.Authenticator(adminToken string) openapi3filter.AuthenticationFunc`
  - Tipos gerados: `openapi.Endpoint`, `openapi.EndpointWithSecret`, `openapi.EndpointList`, `openapi.CreateEndpointRequest`, `openapi.UpdateEndpointRequest`, `openapi.ApplicationId`, `openapi.EndpointId`, e os quatro métodos novos de `openapi.ServerInterface`

- [ ] **Step 1: Ligar o spec embutido**

Em `contracts/oapi-codegen.yaml`, troque `embedded-spec: false` por `true`.

O middleware de validação precisa do documento em runtime para saber **quais operações exigem `AdminToken`**. É isso que faz a autenticação vir do contrato em vez de uma lista de rotas escrita à mão — e é o que `internal/CLAUDE.md` já exige: *"Validação de request vem do contrato."*

- [ ] **Step 2: Gerar e ler o que saiu**

```bash
go tool oapi-codegen -config contracts/oapi-codegen.yaml contracts/openapi.yaml
```

**Abra `internal/openapi/openapi.gen.go`** e anote:

- Os nomes exatos dos structs de `Endpoint`, `EndpointWithSecret`, `EndpointList`, `CreateEndpointRequest`, `UpdateEndpointRequest`.
- Se `ApplicationId` e `EndpointId` viraram alias de `string` ou tipo nomeado.
- A assinatura exata dos quatro métodos novos de `ServerInterface` — em especial **como os parâmetros de caminho chegam**.
- O nome da função que devolve o `*openapi3.T` embutido (algo como `GetSwagger`).

Use esses nomes nas tasks seguintes. **Não edite o gerado.**

- [ ] **Step 3: Adicionar o middleware de validação**

```bash
go get github.com/oapi-codegen/nethttp-middleware@latest
go mod tidy
```

**Leia a API do pacote antes de escrever código** — `go doc github.com/oapi-codegen/nethttp-middleware`. O plano assume `OapiRequestValidatorWithOptions(spec *openapi3.T, opts *Options) func(http.Handler) http.Handler`, com `Options.Options.AuthenticationFunc`. Se a forma for outra, **adapte e reporte**.

- [ ] **Step 4: Escrever o teste da autenticação**

Stub `internal/httpauth/admin.go` com só `package httpauth`.

`internal/httpauth/admin_test.go`:

```go
package httpauth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/httpauth"
)

const expected = "s3cr3t-admin-token"

func request(header string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/applications/app_1/endpoints", nil)
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	return r
}

func TestCheckAdminTokenAcceptsTheRightToken(t *testing.T) {
	if err := httpauth.CheckAdminToken(request("Bearer "+expected), expected); err != nil {
		t.Errorf("error = %v, queria nil", err)
	}
}

// Todas as recusas devolvem o MESMO erro. Distinguir "ausente" de "errado"
// informaria a um atacante que o formato do envio já está certo.
func TestEveryRejectionIsIndistinguishable(t *testing.T) {
	for _, tt := range []struct{ name, header string }{
		{"sem header", ""},
		{"header vazio", " "},
		{"sem o esquema Bearer", expected},
		{"esquema errado", "Basic " + expected},
		{"token errado", "Bearer errado"},
		{"token vazio", "Bearer "},
		{"prefixo certo do token", "Bearer s3cr3t"},
		{"token com sufixo a mais", "Bearer " + expected + "x"},
		{"caixa diferente no esquema", "bearer " + expected},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := httpauth.CheckAdminToken(request(tt.header), expected)
			if !errors.Is(err, errs.InvalidCredentials) {
				t.Errorf("error = %v, queria errs.InvalidCredentials", err)
			}
		})
	}
}

// Um token esperado vazio nunca pode aceitar nada: seria uma API aberta por
// causa de uma variável de ambiente não preenchida.
func TestAnEmptyExpectedTokenAcceptsNothing(t *testing.T) {
	for _, header := range []string{"", "Bearer ", "Bearer qualquercoisa"} {
		if err := httpauth.CheckAdminToken(request(header), ""); !errors.Is(err, errs.InvalidCredentials) {
			t.Errorf("header %q: error = %v, queria errs.InvalidCredentials", header, err)
		}
	}
}
```

Sobre `bearer` minúsculo: o RFC 7235 diz que o esquema é case-insensitive. O teste acima o trata como recusa, o que é **mais estrito que o RFC**. Isso é escolha, e a implementação precisa combinar com o teste — se preferir aceitar, mova essa linha para `TestCheckAdminTokenAcceptsTheRightToken` e ajuste a implementação. Reporte qual você seguiu.

- [ ] **Step 5: Rodar para ver falhar**

```bash
go test ./internal/httpauth/ -v
```

Esperado: FAIL na compilação, `undefined: httpauth.CheckAdminToken`.

- [ ] **Step 6: Implementar**

`internal/httpauth/admin.go`:

```go
// Package httpauth verifies the credentials of inbound requests.
package httpauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/victorzix/vhook/internal/errs"
)

const scheme = "Bearer "

// CheckAdminToken verifies the management credential.
//
// Both sides are hashed before comparing. subtle.ConstantTimeCompare is only
// constant-time for equal lengths — it returns early when they differ — so
// comparing digests keeps the timing flat whatever the attacker sends. A plain
// == would return at the first differing byte and leak the token prefix.
func CheckAdminToken(r *http.Request, expected string) error {
	// An unset expected token must never authorise anything: an API left open
	// by an empty environment variable is worse than one that refuses everyone.
	if expected == "" {
		return errors.Join(errs.InvalidCredentials,
			errors.New("httpauth: no admin token configured"))
	}

	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, scheme) {
		return errors.Join(errs.InvalidCredentials,
			errors.New("httpauth: missing or malformed authorization header"))
	}

	got := sha256.Sum256([]byte(strings.TrimPrefix(header, scheme)))
	want := sha256.Sum256([]byte(expected))
	if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
		// Same error as every other rejection, on purpose.
		return errors.Join(errs.InvalidCredentials, errors.New("httpauth: invalid admin token"))
	}
	return nil
}
```

- [ ] **Step 7: Escrever o adaptador para o validador**

No mesmo arquivo, a função que liga o contrato à verificação. **Ajuste ao que o Step 3 mostrou.**

```go
// Authenticator dispatches on the security scheme declared in the contract.
// Adding a route with `security: [AdminToken]` protects it automatically —
// forgetting to protect a route stops being possible, because the contract
// says so and this function reads the contract.
func Authenticator(adminToken string) openapi3filter.AuthenticationFunc {
	return func(_ context.Context, in *openapi3filter.AuthenticationInput) error {
		switch in.SecuritySchemeName {
		case "AdminToken":
			return CheckAdminToken(in.RequestValidationInput.Request, adminToken)
		default:
			return errors.Join(errs.InvalidCredentials,
				fmt.Errorf("httpauth: unknown security scheme %q", in.SecuritySchemeName))
		}
	}
}
```

Imports novos: `"context"`, `"fmt"`, `"github.com/getkin/kin-openapi/openapi3filter"`.

- [ ] **Step 8: Rodar para ver passar**

```bash
go test ./internal/httpauth/ -v
go build ./...
```

Esperado: PASS nos três testes e nos onze subtestes; build limpo.

- [ ] **Step 9: Verificar que regerar é estável**

```bash
go tool sqlc generate
go tool oapi-codegen -config contracts/oapi-codegen.yaml contracts/openapi.yaml
git status --short
```

Esperado: nenhum arquivo gerado mudou de conteúdo entre duas gerações.

- [ ] **Step 10: Commit**

```bash
git add contracts internal/openapi internal/httpauth go.mod go.sum
```

```
feat: autenticar management pelo contrato
```

---

## Task 6: Migration e queries

**Files:**
- Create: `migrations/000003_endpoint_url_unique.up.sql`, `migrations/000003_endpoint_url_unique.down.sql`
- Create: `internal/store/queries/endpoints.sql`
- Create: `internal/store/sqlc/endpoints.sql.go` (gerado)
- Create: `internal/store/migrate_endpoints_test.go`

**Interfaces:**
- Produces: `sqlc.LockApplication`, `sqlc.CountEndpoints`, `sqlc.CreateEndpoint`, `sqlc.ListEndpoints`, `sqlc.GetEndpoint`, `sqlc.UpdateEndpointURL` — **nomes previstos; confirme no gerado.**

- [ ] **Step 1: Escrever a migration**

`migrations/000003_endpoint_url_unique.up.sql`:

```sql
-- Dois endpoints com a mesma URL na mesma application receberiam entregas
-- idênticas em duplicata. O índice é regra de domínio, e de quebra faz o
-- clique duplo do dashboard virar 409 em vez de lixo com secret novo.
--
-- Sem CONCURRENTLY porque a tabela está vazia; sobre tabela em uso ele seria
-- obrigatório para não travar escrita.
CREATE UNIQUE INDEX endpoints_application_url_idx
    ON endpoints (application_id, url);
```

`migrations/000003_endpoint_url_unique.down.sql`:

```sql
DROP INDEX IF EXISTS endpoints_application_url_idx;
```

- [ ] **Step 2: Escrever as queries**

`internal/store/queries/endpoints.sql`:

```sql
-- name: LockApplication :one
-- Trava a linha da application para o resto da transação. Tomada ANTES da
-- contagem: sem ela, duas criações simultâneas leem o mesmo total e ambas
-- inserem. É por tenant, então dois clientes não se esperam.
SELECT id FROM applications WHERE id = $1 FOR UPDATE;

-- name: CountEndpoints :one
SELECT count(*) FROM endpoints WHERE application_id = $1;

-- name: CreateEndpoint :one
INSERT INTO endpoints (id, application_id, url, secret_encrypted)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListEndpoints :many
SELECT * FROM endpoints
WHERE application_id = $1
ORDER BY created_at, id;

-- name: GetEndpoint :one
-- O application_id no WHERE é o que faz recurso de outro tenant devolver 404
-- em vez de 403: existência e autorização ficam indistinguíveis de fora.
SELECT * FROM endpoints WHERE id = $1 AND application_id = $2;

-- name: UpdateEndpointURL :one
UPDATE endpoints
SET url = $3, updated_at = now()
WHERE id = $1 AND application_id = $2
RETURNING *;
```

- [ ] **Step 3: Gerar e ler o que saiu**

```bash
go tool sqlc generate
```

Abra `internal/store/sqlc/endpoints.sql.go` e anote as assinaturas reais e os nomes dos structs de params. A spec 002 mostrou que os **nomes** costumam acertar e os **tipos** não: as colunas `uuid` viram `pgtype.UUID`, e `ids.New()` devolve `uuid.UUID`.

- [ ] **Step 4: Escrever o teste do índice**

`internal/store/migrate_endpoints_test.go`:

```go
package store_test

import (
	"context"
	"testing"

	"github.com/victorzix/vhook/internal/store"
)

func TestMigrateCreatesTheEndpointURLIndex(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(t)

	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if !indexNames(t, ctx, url)["endpoints_application_url_idx"] {
		t.Error("falta o índice endpoints_application_url_idx")
	}
}
```

`startPostgres` e `indexNames` já existem no pacote, criados pela spec 001.

- [ ] **Step 5: Rodar os testes de `store`**

```bash
go test ./internal/store/ -v
```

Esperado: PASS em tudo, incluindo `TestRollbackEmptiesTheSchema` — a `000003.down` derruba só o índice.

- [ ] **Step 6: Commit**

```bash
git add migrations internal/store
```

```
feat: impedir url duplicada na mesma application
```

---

## Task 7: `internal/endpoints` — repo e service

Onde a regra mora. O teste que importa é o de concorrência: sem ele, contagem fora da transação não dá sintoma nenhum.

**Files:**
- Create: `internal/endpoints/repo.go`, `internal/endpoints/service.go`, `internal/endpoints/service_test.go`

**Interfaces:**
- Consumes: `secrets.Cipher`, `dispatch.URLGuard`, `tokens.Random`, `ids.New`, `sqlc.*`, `errs.*`
- Produces:
  - `type endpoints.Endpoint struct { ID uuid.UUID; ApplicationID uuid.UUID; URL string; Status string; Secret string; CreatedAt time.Time }`
  - `func endpoints.NewService(pool *pgxpool.Pool, cipher *secrets.Cipher, guard *dispatch.URLGuard) *Service`
  - `func (s *Service) Create(ctx context.Context, appID uuid.UUID, url string) (Endpoint, error)`
  - `func (s *Service) List(ctx context.Context, appID uuid.UUID) ([]Endpoint, error)`
  - `func (s *Service) Get(ctx context.Context, appID, endpointID uuid.UUID) (Endpoint, error)`
  - `func (s *Service) UpdateURL(ctx context.Context, appID, endpointID uuid.UUID, url string) (Endpoint, error)`

`Endpoint` é struct de **domínio**, escrito à mão — nunca o row do sqlc nem o tipo gerado do OpenAPI. `internal/CLAUDE.md`: *"Nunca um struct que serve a duas dessas funções."* `Secret` vem preenchido em `Create` e `Get`, vazio em `List` e `UpdateURL`.

### O que o `sqlc` gerou de fato — verificado na Task 6, não é previsão

```go
func (q *Queries) LockApplication(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
func (q *Queries) CountEndpoints(ctx context.Context, applicationID pgtype.UUID) (int64, error)
func (q *Queries) ListEndpoints(ctx context.Context, applicationID pgtype.UUID) ([]Endpoint, error)

type CreateEndpointParams    struct{ ID, ApplicationID pgtype.UUID; Url string; SecretEncrypted []byte }
type GetEndpointParams       struct{ ID, ApplicationID pgtype.UUID }
type UpdateEndpointURLParams struct{ ID, ApplicationID pgtype.UUID; Url string }

type Endpoint struct {
    ID, ApplicationID   pgtype.UUID
    Url                 string
    SecretEncrypted     []byte
    Status              string
    ConsecutiveFailures int32
    DisabledAt, CreatedAt, UpdatedAt pgtype.Timestamptz
}
```

Quatro pontos que mudam o código do repo:

- **`Url`, não `URL`**, e `SecretEncrypted` é `[]byte` — encaixa direto na saída de `secrets.Seal`.
- **`CountEndpoints`, `ListEndpoints` e `LockApplication` recebem parâmetro solto**, não struct. Só as outras três têm `...Params`.
- **`LockApplication` é `:one`**, então application inexistente devolve `pgx.ErrNoRows` — é assim que o service cunha `APP-NFD-001` sem precisar de query extra.
- **`CreatedAt` é `pgtype.Timestamptz`**, então o domínio lê `row.CreatedAt.Time`.

- [ ] **Step 1: Escrever os testes**

Stubs `repo.go` e `service.go` com só `package endpoints`.

`internal/endpoints/service_test.go`:

```go
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
```

Acrescente os imports `"github.com/jackc/pgx/v5/pgtype"` e `"github.com/jackc/pgx/v5/pgxpool"`.

- [ ] **Step 2: Rodar para ver falhar**

```bash
go test ./internal/endpoints/ -v
```

Esperado: FAIL na compilação, `undefined: endpoints.NewService`.

- [ ] **Step 3: Implementar o repo**

`internal/endpoints/repo.go` traduz entre o domínio e o sqlc, e **não contém regra**. Ele recebe um executor (`sqlc.DBTX`) e não sabe se está dentro de transação — quem conhece a fronteira é o service.

```go
package endpoints

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/victorzix/vhook/internal/store/sqlc"
)

// Endpoint is the domain struct. It is neither the sqlc row nor the generated
// OpenAPI type: reusing either would couple the rule to a wire format.
type Endpoint struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	URL           string
	Status        string
	Secret        string // only filled where the spec says it is returned
	CreatedAt     time.Time
}

func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }
func goUUID(id pgtype.UUID) uuid.UUID { return uuid.UUID(id.Bytes) }

// fromRow converts without the secret: decrypting is the service's job,
// because only it holds the cipher.
func fromRow(row sqlc.Endpoint) Endpoint {
	return Endpoint{
		ID:            goUUID(row.ID),
		ApplicationID: goUUID(row.ApplicationID),
		URL:           row.Url,
		Status:        row.Status,
		CreatedAt:     row.CreatedAt.Time,
	}
}

type repo struct{ q *sqlc.Queries }

func newRepo(db sqlc.DBTX) *repo { return &repo{q: sqlc.New(db)} }

func (r *repo) lockApplication(ctx context.Context, appID uuid.UUID) error {
	_, err := r.q.LockApplication(ctx, pgUUID(appID))
	return err
}

func (r *repo) count(ctx context.Context, appID uuid.UUID) (int64, error) {
	return r.q.CountEndpoints(ctx, pgUUID(appID))
}

func (r *repo) create(ctx context.Context, id, appID uuid.UUID, url string, blob []byte) (sqlc.Endpoint, error) {
	return r.q.CreateEndpoint(ctx, sqlc.CreateEndpointParams{
		ID: pgUUID(id), ApplicationID: pgUUID(appID), Url: url, SecretEncrypted: blob,
	})
}

func (r *repo) list(ctx context.Context, appID uuid.UUID) ([]sqlc.Endpoint, error) {
	return r.q.ListEndpoints(ctx, pgUUID(appID))
}

func (r *repo) get(ctx context.Context, appID, id uuid.UUID) (sqlc.Endpoint, error) {
	return r.q.GetEndpoint(ctx, sqlc.GetEndpointParams{ID: pgUUID(id), ApplicationID: pgUUID(appID)})
}

func (r *repo) updateURL(ctx context.Context, appID, id uuid.UUID, url string) (sqlc.Endpoint, error) {
	return r.q.UpdateEndpointURL(ctx, sqlc.UpdateEndpointURLParams{
		ID: pgUUID(id), ApplicationID: pgUUID(appID), Url: url,
	})
}
```

**Ajuste os nomes de campo aos que o Step 3 da Task 6 mostrou** — `Url` contra `URL`, `SecretEncrypted`, e o tipo de `CreatedAt`.

- [ ] **Step 4: Implementar o service**

`internal/endpoints/service.go`:

```go
package endpoints

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/victorzix/vhook/internal/dispatch"
	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/ids"
	"github.com/victorzix/vhook/internal/secrets"
	"github.com/victorzix/vhook/internal/tokens"
)

const (
	// secretPrefix makes the secret recognisable in the customer's .env.
	secretPrefix = "whsec_"

	// secretLength is 43 because 43 × log2(62) = 256.0 bits.
	secretLength = 43

	// freeplanEndpoints is §4.28. It lives in code and not in the environment:
	// per-tenant behaviour belongs in the database, and this is the plan's
	// definition, not a deployment knob.
	freePlanEndpoints = 2
)

// Service owns the transaction boundary. The repo receives an executor and
// never opens one of its own.
type Service struct {
	pool   *pgxpool.Pool
	cipher *secrets.Cipher
	guard  *dispatch.URLGuard
}

func NewService(pool *pgxpool.Pool, cipher *secrets.Cipher, guard *dispatch.URLGuard) *Service {
	return &Service{pool: pool, cipher: cipher, guard: guard}
}

// Create registers an endpoint and returns its secret, which is the only time
// it comes back alongside creation.
func (s *Service) Create(ctx context.Context, appID uuid.UUID, rawURL string) (Endpoint, error) {
	// Validate before touching the database: a typo must never reach a
	// transaction, and the tests assert no row is written.
	if err := s.guard.Validate(ctx, rawURL); err != nil {
		return Endpoint{}, err
	}

	id, err := ids.New()
	if err != nil {
		return Endpoint{}, fmt.Errorf("endpoints: new id: %w", err)
	}
	secret, err := tokens.Random(secretPrefix, secretLength)
	if err != nil {
		return Endpoint{}, err
	}
	// The AAD is the endpoint's external id: a blob moved to another row
	// fails to open instead of opening fine.
	blob, err := s.cipher.Seal([]byte(secret), []byte(ids.Encode(ids.Endpoint, id)))
	if err != nil {
		return Endpoint{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()

	r := newRepo(tx)

	// The lock comes BEFORE the count. Without it two concurrent creates read
	// the same total and both insert.
	if err := r.lockApplication(ctx, appID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Endpoint{}, errors.Join(errs.ApplicationNotFound,
				fmt.Errorf("endpoints: application %s", ids.Encode(ids.Application, appID)))
		}
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: lock: %w", err))
	}

	n, err := r.count(ctx, appID)
	if err != nil {
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: count: %w", err))
	}
	if n >= freePlanEndpoints {
		return Endpoint{}, errors.Join(errs.EndpointLimit,
			fmt.Errorf("endpoints: plan allows %d", freePlanEndpoints))
	}

	row, err := r.create(ctx, id, appID, rawURL, blob)
	if err != nil {
		if isUniqueViolation(err) {
			return Endpoint{}, errors.Join(errs.DuplicateEndpoint,
				errors.New("endpoints: url already registered in this application"))
		}
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: create: %w", err))
	}

	if err := tx.Commit(ctx); err != nil {
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: commit: %w", err))
	}

	out := fromRow(row)
	out.Secret = secret
	return out, nil
}

func (s *Service) List(ctx context.Context, appID uuid.UUID) ([]Endpoint, error) {
	rows, err := newRepo(s.pool).list(ctx, appID)
	if err != nil {
		return nil, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: list: %w", err))
	}
	out := make([]Endpoint, 0, len(rows))
	for _, row := range rows {
		// No secret here: the list is the response that shows up in the most
		// places, and revealing belongs to the detail route.
		out = append(out, fromRow(row))
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, appID, id uuid.UUID) (Endpoint, error) {
	row, err := newRepo(s.pool).get(ctx, appID, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Also the answer for "exists, but belongs to another tenant".
			return Endpoint{}, errors.Join(errs.EndpointNotFound,
				fmt.Errorf("endpoints: %s", ids.Encode(ids.Endpoint, id)))
		}
		return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: get: %w", err))
	}

	secret, err := s.cipher.Open(row.SecretEncrypted, []byte(ids.Encode(ids.Endpoint, goUUID(row.ID))))
	if err != nil {
		// Wrong master key, or a blob moved between rows. Never return junk.
		return Endpoint{}, errors.Join(errs.Internal, fmt.Errorf("endpoints: open secret: %w", err))
	}

	out := fromRow(row)
	out.Secret = string(secret)
	return out, nil
}

func (s *Service) UpdateURL(ctx context.Context, appID, id uuid.UUID, rawURL string) (Endpoint, error) {
	if err := s.guard.Validate(ctx, rawURL); err != nil {
		return Endpoint{}, err
	}
	row, err := newRepo(s.pool).updateURL(ctx, appID, id, rawURL)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return Endpoint{}, errors.Join(errs.EndpointNotFound,
				fmt.Errorf("endpoints: %s", ids.Encode(ids.Endpoint, id)))
		case isUniqueViolation(err):
			return Endpoint{}, errors.Join(errs.DuplicateEndpoint,
				errors.New("endpoints: url already registered in this application"))
		default:
			return Endpoint{}, errors.Join(errs.StorageUnavailable, fmt.Errorf("endpoints: update: %w", err))
		}
	}
	return fromRow(row), nil
}

// isUniqueViolation recognises SQLSTATE 23505. Comparing the code and not the
// message keeps this working when Postgres rewords the text.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
```

- [ ] **Step 5: Rodar para ver passar**

```bash
go test ./internal/endpoints/ -v
```

Esperado: PASS em todos. Cada teste sobe o próprio Postgres, então leva minutos.

- [ ] **Step 6: Verificar que a trava é o que segura o limite**

Mova temporariamente a chamada de `r.lockApplication` para **depois** de `r.count`:

```bash
go test ./internal/endpoints/ -run TestConcurrentCreatesStopAtThePlanLimit -v
```

Esperado: FAIL com "criados = 6, queria exatamente 2" ou número parecido. **Restaure a ordem e rode até PASS.** É a inversão que uma refatoração faz sem querer, e sem este teste não haveria sintoma.

- [ ] **Step 7: Commit**

```bash
git add internal/endpoints
```

```
feat: cadastrar endpoint com secret cifrado
```

---

## Task 8: Handler, wiring e ponta a ponta

**Files:**
- Create: `internal/endpoints/handler.go`
- Create: `cmd/api/endpoints_test.go`
- Modify: `cmd/api/server.go`, `cmd/api/config.go`, `cmd/api/config_test.go`, `cmd/api/main.go`, `cmd/api/server_test.go`
- Modify: `internal/obs/health_test.go`
- Modify: `.env.example`, `CLAUDE.md`, `go.mod`, `go.sum`

### O que a Task 5 descobriu, e que muda esta task

Estes são **fatos verificados no gerado**, não previsão. Use-os:

- **`ApplicationId` e `EndpointId` são `= string`**, alias e não tipo nomeado. Não há conversão a fazer — e não há checagem de tipo: passar um id de application onde se espera o de endpoint **compila**. É o parse por prefixo do `internal/ids` que pega isso em runtime.
- **Os parâmetros de caminho chegam como argumentos posicionais** depois de `r`, na ordem da URL. Não existe struct `...Params`, e o wrapper gerado já fez o bind: nada de `chi.URLParam` no handler.
- **A função do spec embutido é `openapi.GetSpec()`.** `GetSwagger()` existe mas está **deprecada**.
- **`EndpointStatus` gerou constantes sem prefixo de tipo**: `openapi.Active` e `openapi.Disabled`. Nomes genéricos — cuidado com colisão.
- **`nethttp-middleware` foi removido do `go.mod` pelo `go mod tidy`** da Task 5, porque nada o importava ainda. **Rode `go get github.com/oapi-codegen/nethttp-middleware@v1.2.0` no começo desta task.** O módulo está no cache local.
- **`ChiServerOptions.ErrorHandlerFunc` tem a forma `func(w http.ResponseWriter, r *http.Request, err error)`.** Sem ela, o default responde `http.Error(w, err.Error(), 400)` — texto livre e fora do envelope de erro do projeto. O erro entregue é `*openapi.InvalidParamFormatError`.
- **`nethttpmiddleware.Options` tem `ErrorHandler func(w, message string, statusCode int)` e `ErrorHandlerWithOpts`**, que recebe o `error` e tem precedência. Prefira o segundo. Há também `SilenceServersWarning` e `DoNotValidateServers`.
- **O middleware entra em pânico** se `gorillamux.NewRouter(spec)` falhar. Construir o spec no boot, e não por requisição, mantém isso no caminho de inicialização.

### Um buraco do plano que a Task 5 expôs

`internal/obs/health_test.go` tem `TestHealthSatisfiesTheGeneratedInterface`, escrito pela spec 001 quando o contrato tinha três operações. Com sete, **`*obs.Health` sozinho nunca mais satisfaz `openapi.ServerInterface`**, e o pacote deixa de compilar.

O teste não estava errado — a asserção é que **alguém** satisfaz o contrato, e esse alguém passou a ser o `apiServer` composto. **Mova-o**: remova de `internal/obs/health_test.go` e recrie em `cmd/api`, afirmando sobre `apiServer`. Deletar sem recriar perderia a garantia de que o handler continua casando com o contrato gerado — que é justamente o que o teste vale.

**Interfaces:**
- Produces: `func endpoints.NewHandler(svc *Service) *Handler`, com os quatro métodos de `openapi.ServerInterface`

- [ ] **Step 1: Acrescentar as duas variáveis de ambiente**

`cmd/api/config.go` ganha `adminToken` e `ssrfAllowlist`:

```go
type config struct {
	databaseURL   string
	rabbitURL     string
	httpAddr      string
	masterKey     []byte
	adminToken    string
	ssrfAllowlist []string
}
```

`VHOOK_ADMIN_TOKEN` e `VHOOK_MASTER_KEY` são **obrigatórias**; ausência devolve `errs.MissingConfig` nomeando a variável, **nunca o valor**. `VHOOK_SSRF_ALLOWLIST` é opcional e vem separada por vírgula.

Acrescente os casos a `cmd/api/config_test.go`, seguindo a tabela que já existe lá.

- [ ] **Step 2: Escrever o handler**

`internal/endpoints/handler.go`. **Ajuste os nomes gerados aos que a Task 5 anotou.** O handler faz três coisas — decodifica, chama o service, serializa — e **zero regra**.

```go
package endpoints

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/ids"
	"github.com/victorzix/vhook/internal/obs"
	"github.com/victorzix/vhook/internal/openapi"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// parsePath turns the external identifiers into UUIDs. A malformed id is a
// 422, distinct from a well-formed id that does not exist, which is a 404.
func parsePath(appID string, endpointID *string) (uuid.UUID, uuid.UUID, error) {
	app, err := ids.Parse(ids.Application, appID)
	if err != nil {
		return uuid.Nil, uuid.Nil, errs.MalformedID
	}
	if endpointID == nil {
		return app, uuid.Nil, nil
	}
	ept, err := ids.Parse(ids.Endpoint, *endpointID)
	if err != nil {
		return uuid.Nil, uuid.Nil, errs.MalformedID
	}
	return app, ept, nil
}

func (h *Handler) CreateEndpoint(w http.ResponseWriter, r *http.Request, applicationId openapi.ApplicationId) {
	appID, _, err := parsePath(string(applicationId), nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}

	var body openapi.CreateEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, r, errs.InvalidEndpointURL)
		return
	}

	created, err := h.svc.Create(r.Context(), appID, body.Url)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWithSecret(created))
}

func (h *Handler) ListEndpoints(w http.ResponseWriter, r *http.Request, applicationId openapi.ApplicationId) {
	appID, _, err := parsePath(string(applicationId), nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	list, err := h.svc.List(r.Context(), appID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out := make([]openapi.Endpoint, 0, len(list))
	for _, e := range list {
		out = append(out, toAPI(e))
	}
	writeJSON(w, http.StatusOK, openapi.EndpointList{Endpoints: out})
}

func (h *Handler) GetEndpoint(w http.ResponseWriter, r *http.Request, applicationId openapi.ApplicationId, endpointId openapi.EndpointId) {
	raw := string(endpointId)
	appID, eptID, err := parsePath(string(applicationId), &raw)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	got, err := h.svc.Get(r.Context(), appID, eptID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toWithSecret(got))
}

func (h *Handler) UpdateEndpoint(w http.ResponseWriter, r *http.Request, applicationId openapi.ApplicationId, endpointId openapi.EndpointId) {
	raw := string(endpointId)
	appID, eptID, err := parsePath(string(applicationId), &raw)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var body openapi.UpdateEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, r, errs.InvalidEndpointURL)
		return
	}
	updated, err := h.svc.UpdateURL(r.Context(), appID, eptID, body.Url)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPI(updated))
}

func toAPI(e Endpoint) openapi.Endpoint {
	return openapi.Endpoint{
		Id:        openapi.EndpointId(ids.Encode(ids.Endpoint, e.ID)),
		Url:       e.URL,
		Status:    openapi.EndpointStatus(e.Status),
		CreatedAt: e.CreatedAt,
	}
}

func toWithSecret(e Endpoint) openapi.EndpointWithSecret {
	return openapi.EndpointWithSecret{
		Id:        openapi.EndpointId(ids.Encode(ids.Endpoint, e.ID)),
		Url:       e.URL,
		Status:    openapi.EndpointStatus(e.Status),
		CreatedAt: e.CreatedAt,
		Secret:    e.Secret,
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeErr maps the registered constant to its status. The handler never picks
// a status ad hoc — that is how the same error returns 400 in one place and
// 422 in another.
func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	var registered *errs.Error
	if !errors.As(err, &registered) {
		// Um erro não registrado nunca vaza detalhe para o cliente: vira
		// SYS-INT-001, e o original fica no log pelo middleware de recover.
		registered = errs.Internal
	}
	obs.WriteError(w, r, registered)
}
```

Imports do arquivo: `"encoding/json"`, `"errors"`, `"net/http"`, mais `uuid`, `errs`, `ids`, `obs` e `openapi`.

- [ ] **Step 3: Montar tudo no router**

`cmd/api/server.go`. A `ServerInterface` gerada agora tem sete métodos: três de health e quatro de endpoints. Um struct que embute os dois satisfaz o conjunto por promoção de método.

```go
// apiServer satisfies the generated ServerInterface by promotion: health owns
// the three operational routes, endpoints the four management ones.
type apiServer struct {
	*obs.Health
	*endpoints.Handler
}

func newRouter(logger *slog.Logger, health *obs.Health, h *endpoints.Handler, adminToken string) (http.Handler, error) {
	spec, err := openapi.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("api: load spec: %w", err)
	}

	r := chi.NewRouter()
	r.Use(obs.Correlation)
	r.Use(obs.RequestLog(logger))
	r.Use(obs.Recover(logger))

	// Validation and authentication both come from the contract. Adding a
	// route with `security: [AdminToken]` protects it automatically; there is
	// no list of protected paths to forget to update.
	r.Use(nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: httpauth.Authenticator(adminToken),
		},
		// The servers block is documentation; validating against it would
		// reject every request that does not carry the declared prefix.
		DoNotValidateServers: true,
		// The validator's own message never reaches the client: our error
		// envelope carries a code and a correlation id, never text.
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, r *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
			if opts.StatusCode == http.StatusUnauthorized || opts.StatusCode == http.StatusForbidden {
				obs.WriteError(w, r, errs.InvalidCredentials)
				return
			}
			obs.WriteError(w, r, errs.MalformedID)
		},
	}))

	// Without this the generated wrapper answers a bad path parameter with
	// http.Error and free text, outside the error envelope.
	return openapi.HandlerWithOptions(apiServer{Health: health, Handler: h}, openapi.ChiServerOptions{
		BaseRouter: r,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			obs.WriteError(w, r, errs.MalformedID)
		},
	}), nil
}
```

**Confira a forma de `ErrorHandlerOpts` e de `HandlerWithOptions` no pacote antes de escrever.** A Task 5 verificou os nomes, mas não exercitou estes dois — adapte ao que existir e reporte.

Em `main.go`, construa `secrets.NewCipher`, `dispatch.NewURLGuard(net.DefaultResolver, cfg.ssrfAllowlist)`, `endpoints.NewService` e `endpoints.NewHandler`, e passe para `newRouter`.

`net.DefaultResolver` satisfaz `dispatch.Resolver`: ele tem `LookupNetIP` com a assinatura da interface.

- [ ] **Step 4: Escrever o teste ponta a ponta**

`cmd/api/endpoints_test.go` monta **o mesmo router de produção** — é isso que faz o teste provar que a autenticação vinda do contrato de fato protege as quatro rotas, e não uma versão de teste que passa por fora.

```go
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

	"github.com/victorzix/vhook/internal/obs"
)

const testAdminToken = "token-de-teste-do-management"

// harness sobe Postgres, migra, semeia uma application e devolve o servidor
// HTTP montado com o router de produção, mais o id externo da application.
type harness struct {
	server *httptest.Server
	appID  string
}

func newHarness(t *testing.T) harness {
	t.Helper()
	if testing.Short() {
		t.Skip("integração: precisa de Docker")
	}
	// Reusa o mesmo caminho do server_test.go da spec 001: container,
	// migrations, pool. Extraia aquele trecho para um helper compartilhado se
	// ainda não estiver.
	dbURL := startMigratedPostgres(t)
	appID := seedApplication(t, dbURL)

	logger := obs.NewLogger(io.Discard, slog.LevelError)
	router := buildRouterForTest(t, logger, dbURL, testAdminToken)

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return harness{server: server, appID: appID}
}

// do executa uma requisição já autenticada, salvo quando token == "".
func (h harness) do(t *testing.T, method, path, token, body string) *http.Response {
	t.Helper()
	var rdr io.Reader
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
	if body.Error.CorrelationID == "" {
		t.Error("resposta de erro sem correlation_id")
	}
	// A resposta de erro nunca carrega mensagem: o dashboard traduz o código.
	if strings.Contains(string(raw), "\"message\"") {
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

func TestCreateReturnsTheSecretAndListDoesNot(t *testing.T) {
	h := newHarness(t)
	path := "/v1/applications/" + h.appID + "/endpoints"

	res := h.do(t, http.MethodPost, path, bearer(), `{"url":"https://api.exemplo.com/hooks"}`)
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
		t.Errorf("o detalhe devolveu secret diferente do da criação")
	}
}

func TestCreateRefusesBeyondThePlanLimit(t *testing.T) {
	h := newHarness(t)
	path := "/v1/applications/" + h.appID + "/endpoints"

	for _, suffix := range []string{"a", "b"} {
		res := h.do(t, http.MethodPost, path, bearer(),
			`{"url":"https://api.exemplo.com/hooks/`+suffix+`"}`)
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d, want 201", res.StatusCode)
		}
	}

	res := h.do(t, http.MethodPost, path, bearer(), `{"url":"https://api.exemplo.com/hooks/c"}`)
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
	const body = `{"url":"https://api.exemplo.com/hooks"}`

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
		{"http", "http://api.exemplo.com/hooks", "EPT-VAL-001"},
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

	res := h.do(t, http.MethodPost, path, bearer(), `{"url":"https://api.exemplo.com/hooks"}`)
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

	res := h.do(t, http.MethodPost, path, bearer(), `{"url":"https://api.exemplo.com/hooks"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}
	created := decodeEndpoint(t, res)
	id, _ := created["id"].(string)
	secret, _ := created["secret"].(string)

	res = h.do(t, http.MethodPatch, path+"/"+id, bearer(), `{"url":"https://api.exemplo.com/v2/hooks"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	patched := decodeEndpoint(t, res)
	if patched["url"] != "https://api.exemplo.com/v2/hooks" {
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
```

Três helpers ficam por escrever, e é deliberado que você os extraia em vez de duplicar: `startMigratedPostgres(t) string`, que é o trecho de container e migrations já presente em `cmd/api/server_test.go`; `seedApplication(t, dbURL) string`, que insere organização e application e devolve o id externo; e `seedApplicationIn(t, harness) string`, que cria uma segunda application no mesmo banco. `buildRouterForTest` monta exatamente o que `main.go` monta — se ele divergir do wiring de produção, o teste deixa de provar o que promete.

O resolver aqui é o real: `169.254.169.254` e `10.0.0.1` são literais de IP, então não há DNS a consultar e o teste não depende de rede.

- [ ] **Step 5: Rodar para ver falhar, depois passar**

```bash
go test ./cmd/api/ -v
```

Primeiro FAIL enquanto o wiring não existir, depois PASS em todos.

- [ ] **Step 6: Atualizar `.env.example` e a tabela de comandos**

```
# Token de serviço do management. O BFF do dashboard o usa server-side;
# ele nunca chega ao browser. Gere um com `go run ./cmd/adminctl genkey`.
VHOOK_ADMIN_TOKEN=

# Hostnames que pulam a checagem de faixa de IP, separados por vírgula.
# Existe para o sink do compose, que resolve para endereço privado de propósito.
# NÃO aceita CIDR: liberar uma faixa seria um buraco maior que o problema.
VHOOK_SSRF_ALLOWLIST=sink
```

- [ ] **Step 7: Suíte inteira**

```bash
go tool sqlc generate
go tool oapi-codegen -config contracts/oapi-codegen.yaml contracts/openapi.yaml
git diff --exit-code
go vet ./...
gofmt -l .
go tool golangci-lint run ./...
go test -shuffle=on ./...
```

Esperado: sem diff, sem apontamento, PASS em todos os pacotes.

- [ ] **Step 8: Verificação manual**

```bash
docker compose up -d postgres rabbitmq
export DATABASE_URL='postgres://vhook:vhook@localhost:55432/vhook?sslmode=disable'
export VHOOK_MASTER_KEY="$(go run ./cmd/adminctl genkey)"
export VHOOK_ADMIN_TOKEN="$(go run ./cmd/adminctl genkey)"
export VHOOK_SSRF_ALLOWLIST=sink

go run ./cmd/adminctl bootstrap        # anote o app_...
go run ./cmd/api &

APP=app_...   # do output do bootstrap
curl -i -X POST localhost:8080/v1/applications/$APP/endpoints \
  -H "Authorization: Bearer $VHOOK_ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"url":"https://api.exemplo.com/hooks"}'          # 201 com secret

curl -i localhost:8080/v1/applications/$APP/endpoints    # 401, sem Authorization

curl -i -X POST localhost:8080/v1/applications/$APP/endpoints \
  -H "Authorization: Bearer $VHOOK_ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"url":"http://169.254.169.254/latest/meta-data/"}'   # 422 EPT-VAL-001
```

Confirme no `psql` que `secret_encrypted` não contém o secret em claro.

- [ ] **Step 9: Commit**

```bash
git add internal/endpoints cmd/api .env.example CLAUDE.md
```

```
feat: expor as rotas de cadastro de endpoint
```

Este é o `feat:` que corta a `v0.3.0`.

---

## Encerramento da spec

**Dois modos de falha da spec ficam declaradamente sem teste automatizado**, e é escolha, não esquecimento:

- **"Postgres cai no meio da criação"** — a atomicidade vem da transação, mecanismo já provado por teste na spec 002, que derrubou a tabela `applications` para forçar falha entre dois inserts. Repetir aqui exigiria derrubar `endpoints`, que é a tabela que o fixture inteiro usa.
- **Comparação em tempo constante** — teste de timing é ruidoso e falha por carga da máquina, não por regressão. O que dá para testar é o observável, e está testado: toda recusa devolve o mesmo código, indistinguível.

- [ ] **Escrever o `result.md`** com o template de `docs/specs/_template_/result.md`.

Candidatos prováveis de divergência, porque o plano os assume sem poder verificar:

- a API de `oapi-codegen/nethttp-middleware` — nome da função de validação, forma de `Options`, assinatura de `ErrorHandlerFunc`;
- se `GetSwagger` é mesmo o nome da função do spec embutido, e se limpar `spec.Servers` é necessário;
- como os parâmetros de caminho chegam aos métodos gerados;
- os nomes e tipos dos campos gerados pelo `sqlc` para `endpoints`;
- se `net.DefaultResolver` satisfaz `dispatch.Resolver` sem adaptador.

- [ ] **Atualizar o índice** em `docs/specs/README.md`: status da 003 para `implementada`.
