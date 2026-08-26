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
