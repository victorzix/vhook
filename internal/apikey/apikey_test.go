package apikey_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/victorzix/vhook/internal/apikey"
	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/tokens"
)

// Chaves mestras fixas: o teste do pepper precisa de duas que difiram.
var (
	masterA = []byte("0123456789abcdef0123456789abcdef")
	masterB = []byte("fedcba9876543210fedcba9876543210")
)

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
		if !strings.ContainsRune(tokens.Alphabet, c) {
			t.Errorf("caractere %d é %q, fora do alfabeto base62", i, c)
		}
	}
}

func TestHashIsDeterministic(t *testing.T) {
	h := newHasher(t, masterA)
	const key = "vhk_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H"

	// As duas chamadas ficam em variáveis porque `h.Hash(key) != h.Hash(key)`
	// escrito inline é SA4000 para o staticcheck — ele lê como expressão
	// idêntica dos dois lados. A asserção é a mesma: duas invocações
	// independentes, e um Hash com salt aleatório por chamada divergiria aqui.
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
