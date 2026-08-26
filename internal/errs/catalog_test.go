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
