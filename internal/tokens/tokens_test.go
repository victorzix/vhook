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
